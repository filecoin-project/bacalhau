package transport

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/filecoin-project/bacalhau/pkg/executor"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type WriteEventHandlerFn func(ctx context.Context, event *executor.JobEvent) error

// GenericTransport is a generic base transport layer that handles a bunch of
// boilerplate for a parent transport. The parent transport just has to provide
// a WriteEventHandlerFn for broadcasting messages to other bacalhau nodes, and
// call the appropriate GenericTransport functions when messages are received
// from other bacalhau nodes.
type GenericTransport struct {
	NodeId string
	Jobs   map[string]*executor.Job
	Mutex  sync.Mutex
	// the list of functions to call when we get an update about a job
	SubscribeFuncs    []func(jobEvent *executor.JobEvent, job *executor.Job)
	WriteEventHandler WriteEventHandlerFn
}

func NewGenericTransport(nodeID string,
	writeEventHandler WriteEventHandlerFn) *GenericTransport {

	return &GenericTransport{
		NodeId:            nodeID,
		Jobs:              make(map[string]*executor.Job),
		SubscribeFuncs:    []func(jobEvent *executor.JobEvent, job *executor.Job){},
		WriteEventHandler: writeEventHandler,
	}
}

func (transport *GenericTransport) writeEvent(ctx context.Context,
	event *executor.JobEvent) error {

	if event.NodeId == "" {
		event.NodeId = gt.NodeID
	}

	return gt.writeEventHandler(ctx, event)
}

func (transport *GenericTransport) BroadcastEvent(event *executor.JobEvent) {
	transport.Mutex.Lock()
	defer transport.Mutex.Unlock()

	gt.mutex.Lock()
	defer gt.mutex.Unlock()

	if _, ok := transport.Jobs[event.JobId]; !ok {
		transport.Jobs[event.JobId] = &executor.Job{
			Id:        event.JobId,
			Owner:     event.NodeId,
			Spec:      nil,
			Deal:      nil,
			State:     make(map[string]*executor.JobState),
			CreatedAt: time.Now(),
		}
	}

	// Passed in for create and update events:
	if event.JobSpec != nil {
		gt.jobs[event.JobId].Spec = event.JobSpec
	}

	// Keep track of job owner so we know who can edit a job:
	if event.JobDeal != nil {
		gt.jobs[event.JobId].Deal = event.JobDeal
	}

	// Jobs have different states on different nodes:
	if event.JobState != nil && event.NodeId != "" {
		gt.jobs[event.JobId].State[event.NodeId] = event.JobState
	}

	// Attach metadata to local job lifecycle context:
	jobCtx := gt.getJobNodeContext(ctx, event.JobId)
	gt.addJobLifecycleEvent(jobCtx, event.JobId,
		fmt.Sprintf("receive_%s", event.EventName))

	// If the event is known to be terminal, end the lifecycle context:
	if event.EventName.IsTerminal() {
		gt.endJobContext(event.JobId)
	}

	// Actually notify in-process listeners:
	for _, subscribeFunc := range gt.SubscribeFuncs {
		go subscribeFunc(jobCtx, event, gt.jobs[event.JobId])
	}
}

/////////////////////////////////////////////////////////////
/// LIFECYCLE
/////////////////////////////////////////////////////////////

func (gt *GenericTransport) Start(ctx context.Context) error {
	panic("should be implemented by parent transport")
}

func (gt *GenericTransport) Shutdown(ctx context.Context) error {
	// End all job lifecycle spans so we don't lose any tracing data:
	for _, ctx := range gt.jobContexts {
		trace.SpanFromContext(ctx).End()
	}
	for _, ctx := range gt.jobNodeContexts {
		trace.SpanFromContext(ctx).End()
	}

	return nil
}

func (gt *GenericTransport) HostID(ctx context.Context) (
	string, error) {

	panic("should be implemented by parent transport")
}

/////////////////////////////////////////////////////////////
/// READ OPERATIONS
/////////////////////////////////////////////////////////////

func (transport *GenericTransport) List(ctx context.Context) (
	ListResponse, error) {

	return ListResponse{
		Jobs: transport.Jobs,
	}, nil
}

func (transport *GenericTransport) Get(ctx context.Context, id string) (
	*executor.Job, error) {

	job, ok := gt.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job not found in transport: %s", id)
	}

func (transport *GenericTransport) Subscribe(ctx context.Context,
	subscribeFunc func(jobEvent *executor.JobEvent, job *executor.Job)) {

func (gt *GenericTransport) Subscribe(ctx context.Context, fn SubscribeFn) {
	gt.SubscribeFuncs = append(gt.SubscribeFuncs, fn)
}

/////////////////////////////////////////////////////////////
/// WRITE OPERATIONS - "CLIENT" / REQUESTER NODE
/////////////////////////////////////////////////////////////

func (transport *GenericTransport) SubmitJob(ctx context.Context,
	spec *executor.JobSpec, deal *executor.JobDeal) (*executor.Job, error) {

	jobUuid, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("error creating job id: %w", err)
	}
	jobID := jobUuid.String()

	err = transport.writeEvent(ctx, &executor.JobEvent{
		JobId:     jobID,
		EventName: executor.JobEventCreated,
		JobSpec:   spec,
		JobDeal:   deal,
		EventTime: time.Now(),
	}); err != nil {
		return nil, fmt.Errorf("error writing job event: %w", err)
	}

	return &executor.Job{
		Id:        jobID,
		Spec:      spec,
		Deal:      deal,
		State:     make(map[string]*executor.JobState),
		CreatedAt: time.Now(),
	}, nil
}

func (transport *GenericTransport) UpdateDeal(ctx context.Context,
	jobID string, deal *executor.JobDeal) error {

	return transport.writeEvent(ctx, &executor.JobEvent{
		JobId:     jobID,
		EventName: executor.JobEventDealUpdated,
		JobDeal:   deal,
		EventTime: time.Now(),
	})
}

func (gt *GenericTransport) CancelJob(ctx context.Context,
	jobID string) error {

	panic("should be implemented by parent transport")
}

func (gt *GenericTransport) AcceptJobBid(ctx context.Context,
	jobID, nodeID string) error {

	ctx = gt.getJobNodeContext(ctx, jobID)
	gt.addJobLifecycleEvent(ctx, jobID, "write_AcceptJobBid")

	job, err := gt.Get(ctx, jobID)
	if err != nil {
		return err
	}

	job.Deal.AssignedNodes = append(job.Deal.AssignedNodes, nodeID)
	return transport.writeEvent(ctx, &executor.JobEvent{
		JobId:     jobID,
		NodeId:    nodeID,
		EventName: executor.JobEventBidAccepted,
		JobDeal:   job.Deal,
		JobState: &executor.JobState{
			State: executor.JobStateRunning,
		},
		EventTime: time.Now(),
	})
}

func (gt *GenericTransport) RejectJobBid(ctx context.Context,
	jobID, nodeID, message string) error {

	if message == "" {
		message = "Job bid rejected by client."
	}

	return transport.writeEvent(ctx, &executor.JobEvent{
		JobId:     jobID,
		NodeId:    nodeID,
		EventName: executor.JobEventBidRejected,
		JobState: &executor.JobState{
			State:  executor.JobStateBidRejected,
			Status: message,
		},
		EventTime: time.Now(),
	})
}

/////////////////////////////////////////////////////////////
/// WRITE OPERATIONS - "SERVER" / COMPUTE NODE
/////////////////////////////////////////////////////////////

func (gt *GenericTransport) BidJob(ctx context.Context,
	jobID string) error {

	return transport.writeEvent(ctx, &executor.JobEvent{
		JobId:     jobID,
		EventName: executor.JobEventBid,
		JobState: &executor.JobState{
			State: executor.JobStateBidding,
		},
		EventTime: time.Now(),
	})
}

func (gt *GenericTransport) SubmitResult(ctx context.Context,
	jobID, status, resultsID string) error {

	return transport.writeEvent(ctx, &executor.JobEvent{
		JobId:     jobID,
		EventName: executor.JobEventResults,
		JobState: &executor.JobState{
			State:     executor.JobStateComplete,
			Status:    status,
			ResultsId: resultsID,
		},
		EventTime: time.Now(),
	})
}

func (gt *GenericTransport) ErrorJob(ctx context.Context,
	jobID, status string) error {

	return transport.writeEvent(ctx, &executor.JobEvent{
		JobId:     jobID,
		EventName: executor.JobEventError,
		JobState: &executor.JobState{
			State:  executor.JobStateError,
			Status: status,
		},
		EventTime: time.Now(),
	})
}

// this is when the requester node needs to error the status for a node
// for example - results have been given by the compute node
// and in checking the results, the requester node came across some kind of error
// we need to flag that error against the node that submitted the results
// (but we are the requester node) - so we need this util function
func (gt *GenericTransport) ErrorJobForNode(ctx context.Context,
	jobID, nodeID, status string) error {

	return transport.writeEvent(ctx, &executor.JobEvent{
		JobId:     jobID,
		NodeId:    nodeID,
		EventName: executor.JobEventError,
		JobState: &executor.JobState{
			State:  executor.JobStateError,
			Status: status,
		},
		EventTime: time.Now(),
	})
}

// endJobContext ends the local and global lifecycle contexts for a job.
func (gt *GenericTransport) endJobContext(jobID string) {
	ctx := gt.getJobNodeContext(context.Background(), jobID)
	trace.SpanFromContext(ctx).End()

	ctx = gt.getJobContext(jobID)
	trace.SpanFromContext(ctx).End()
}

// getJobContext returns a context that tracks the global lifecycle of a job
// as it is processed by this and other nodes in the bacalhau network.
func (gt *GenericTransport) getJobContext(
	jobID string) context.Context {

	jobCtx, ok := gt.jobContexts[jobID]
	if !ok {
		return context.Background() // no lifecycle context yet
	}
	return jobCtx
}

// getJobNodeContext returns a context that tracks the local lifecycle of a
// job as it has been processed by this node.
func (gt *GenericTransport) getJobNodeContext(ctx context.Context,
	jobID string) context.Context {

	jobCtx, ok := gt.jobNodeContexts[jobID]
	if !ok {
		jobCtx, _ = system.Span(ctx, "transport/generic_transport",
			"JobLifecycle-"+gt.NodeID[:8],
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(
				attribute.String("job_id", jobID),
				attribute.String("node_id", gt.NodeID),
			),
		)

		gt.jobNodeContexts[jobID] = jobCtx
	}
	return jobCtx
}

func (gt *GenericTransport) addJobLifecycleEvent(ctx context.Context,
	jobID, eventName string, attrs ...attribute.KeyValue) {

	span := trace.SpanFromContext(ctx)
	span.AddEvent(eventName,
		trace.WithAttributes(
			append(attrs,
				attribute.String("jobID", jobID),
				attribute.String("nodeID", gt.NodeID),
			)...,
		),
	)
}

func (gt *GenericTransport) newRootSpanForJob(ctx context.Context,
	jobID string) (context.Context, trace.Span) {

	return system.Span(ctx, "transport/generic_transport", "JobLifecycle",
		// job lifecycle spans go in their own, dedicated trace
		trace.WithNewRoot(),

		trace.WithLinks(trace.LinkFromContext(ctx)), // link to any api traces
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("jobID", jobID),
			attribute.String("nodeID", gt.NodeID),
		),
	)
}

// Compile-time interface check:
var _ Transport = (*GenericTransport)(nil)