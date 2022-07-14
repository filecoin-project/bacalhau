package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/filecoin-project/bacalhau/pkg/executor"
)

/*

  event handlers

*/

// first process the event locally and then broadcast it to the network
func (ctrl *Controller) writeEvent(ctx context.Context, ev executor.JobEvent) error {
	jobCtx := ctrl.getJobNodeContext(ctx, ev.JobID)

	// process the event locally
	err := ctrl.handleEvent(jobCtx, ev)
	if err != nil {
		return err
	}

	// tell the rest of the network about the event via the transport
	err = ctrl.transport.Publish(jobCtx, ev)
	if err != nil {
		return err
	}

	return nil
}

// this is called both locally and remotely
// for the node that created the event - it calls this before attenpting
// to transmit the event to other nodes
//
// do these things in this order:
//   * apply the event to the state machine to check validity
//   * mutate the job in the local datastore
//   * add the job event to the local datastore
//   * call our subscribers with the event
func (ctrl *Controller) handleEvent(ctx context.Context, ev executor.JobEvent) error {
	jobCtx := ctrl.handleOtelReadEvent(ctx, ev)

	err := ctrl.validateStateTransition(jobCtx, ev)
	if err != nil {
		return fmt.Errorf("error validateStateTransition: %s", err)
	}

	err = ctrl.mutateDatastore(jobCtx, ev)
	if err != nil {
		return fmt.Errorf("error mutateDatastore: %s", err)
	}

	// now trigger our local subscribers with this event
	ctrl.callLocalSubscribers(jobCtx, ev)

	return nil
}

/*

  process event helpers

*/

// check the state transition of the job is allowed based on the current local state
func (ctrl *Controller) validateStateTransition(ctx context.Context, ev executor.JobEvent) error {
	return nil
}

// mutate the datastore with the given event
func (ctrl *Controller) mutateDatastore(ctx context.Context, ev executor.JobEvent) error {
	var err error

	// work out which internal handler function based on the event type
	switch ev.EventName {

	case executor.JobEventCreated:
		err = ctrl.datastore.AddJob(ctx, constructJob(ev))

	case executor.JobEventDealUpdated:
		err = ctrl.datastore.UpdateJobDeal(ctx, ev.JobID, ev.JobDeal)

	default:
		err = fmt.Errorf("unhandled event type: %s", ev.EventName)
	}

	if err != nil {
		return err
	}

	err = ctrl.datastore.AddEvent(ctx, ev.JobID, ev)
	if err != nil {
		return err
	}

	return nil
}

// trigger the local subscriptions of the compute and requestor nodes
func (ctrl *Controller) callLocalSubscribers(ctx context.Context, ev executor.JobEvent) {
	ctrl.subscribeMutex.Lock()
	defer ctrl.subscribeMutex.Unlock()
	for _, fn := range ctrl.subscribeFuncs {
		go fn(ctx, ev)
	}
}

/*

  utils

*/

func (ctrl *Controller) constructEvent(jobID string, eventName executor.JobEventType) executor.JobEvent {
	return executor.JobEvent{
		SourceNodeID: ctrl.id,
		JobID:        jobID,
		EventName:    eventName,
		EventTime:    time.Now(),
	}
}

func constructJob(ev executor.JobEvent) executor.Job {
	return executor.Job{
		ID:        ev.JobID,
		Spec:      ev.JobSpec,
		Deal:      ev.JobDeal,
		State:     map[string]executor.JobState{},
		CreatedAt: time.Now(),
	}
}