package inmemory

import (
	"context"
	"fmt"
	"sync"

	"github.com/filecoin-project/bacalhau/pkg/executor"
	"github.com/filecoin-project/bacalhau/pkg/localdb"
)

type InMemoryDatastore struct {
	// we keep pointers to these things because we will update them partially
	jobs        map[string]*executor.Job
	states      map[string]map[string]*executor.JobState
	events      map[string][]executor.JobEvent
	localEvents map[string][]executor.JobLocalEvent
	mtx         sync.Mutex
}

func NewInMemoryDatastore() (*InMemoryDatastore, error) {
	res := &InMemoryDatastore{
		jobs:        map[string]*executor.Job{},
		states:      map[string]map[string]*executor.JobState{},
		events:      map[string][]executor.JobEvent{},
		localEvents: map[string][]executor.JobLocalEvent{},
	}
	return res, nil
}

func (d *InMemoryDatastore) GetJob(ctx context.Context, id string) (executor.Job, error) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	job, ok := d.jobs[id]
	if !ok {
		return executor.Job{}, fmt.Errorf("no job found: %s", id)
	}
	return *job, nil
}

func (d *InMemoryDatastore) GetJobEvents(ctx context.Context, id string) ([]executor.JobEvent, error) {
	_, ok := d.jobs[id]
	if !ok {
		return []executor.JobEvent{}, fmt.Errorf("no job found: %s", id)
	}
	result, ok := d.events[id]
	if !ok {
		result = []executor.JobEvent{}
	}
	return result, nil
}

func (d *InMemoryDatastore) GetJobLocalEvents(ctx context.Context, id string) ([]executor.JobLocalEvent, error) {
	_, ok := d.jobs[id]
	if !ok {
		return []executor.JobLocalEvent{}, fmt.Errorf("no job found: %s", id)
	}
	result, ok := d.localEvents[id]
	if !ok {
		result = []executor.JobLocalEvent{}
	}
	return result, nil
}

func (d *InMemoryDatastore) GetJobs(ctx context.Context, query localdb.JobQuery) ([]executor.Job, error) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	result := []executor.Job{}
	if query.ID != "" {
		job, err := d.GetJob(ctx, query.ID)
		if err != nil {
			return result, err
		}
		result = append(result, job)
	} else {
		for _, job := range d.jobs {
			result = append(result, *job)
		}
	}
	return result, nil
}

func (d *InMemoryDatastore) AddJob(ctx context.Context, job executor.Job) error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.jobs[job.ID] = &job
	return nil
}

func (d *InMemoryDatastore) AddEvent(ctx context.Context, jobID string, ev executor.JobEvent) error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	_, ok := d.jobs[jobID]
	if !ok {
		return fmt.Errorf("no job found: %s", jobID)
	}
	eventArr, ok := d.events[jobID]
	if !ok {
		eventArr = []executor.JobEvent{}
	}
	eventArr = append(eventArr, ev)
	d.events[jobID] = eventArr
	return nil
}

func (d *InMemoryDatastore) AddLocalEvent(ctx context.Context, jobID string, ev executor.JobLocalEvent) error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	_, ok := d.jobs[jobID]
	if !ok {
		return fmt.Errorf("no job found: %s", jobID)
	}
	eventArr, ok := d.localEvents[jobID]
	if !ok {
		eventArr = []executor.JobLocalEvent{}
	}
	eventArr = append(eventArr, ev)
	d.localEvents[jobID] = eventArr
	return nil
}

func (d *InMemoryDatastore) UpdateJobDeal(ctx context.Context, jobID string, deal executor.JobDeal) error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	job, ok := d.jobs[jobID]
	if !ok {
		return fmt.Errorf("no job found: %s", jobID)
	}
	job.Deal = deal
	return nil
}

func (d *InMemoryDatastore) GetExecutionStates(ctx context.Context, jobID string) (map[string]executor.JobState, error) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	states := map[string]executor.JobState{}
	jobStates, ok := d.states[jobID]
	if !ok {
		return states, nil
	}
	for nodeID, state := range jobStates {
		states[nodeID] = *state
	}
	return states, nil
}

func (d *InMemoryDatastore) UpdateExecutionState(ctx context.Context, jobID, nodeID string, state executor.JobState) error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	_, ok := d.jobs[jobID]
	if !ok {
		return fmt.Errorf("no job found: %s", jobID)
	}
	jobStates, ok := d.states[jobID]
	if !ok {
		jobStates = map[string]*executor.JobState{}
	}
	nodeState, ok := jobStates[nodeID]
	if !ok {
		nodeState = &state
	}
	nodeState.State = state.State
	if state.ResultsID != "" {
		nodeState.ResultsID = state.ResultsID
	}
	if state.Status != "" {
		nodeState.Status = state.Status
	}
	jobStates[nodeID] = nodeState
	d.states[jobID] = jobStates
	return nil
}

// Static check to ensure that Transport implements Transport:
var _ localdb.LocalDB = (*InMemoryDatastore)(nil)
