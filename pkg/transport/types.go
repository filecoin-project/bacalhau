package transport

import (
	"context"

	"github.com/filecoin-project/bacalhau/pkg/executor"
)

// SubscribeFn is provided by an in-process listener as an event callback.
type SubscribeFn func(context.Context, executor.JobEvent)

// Transport is an interface representing a communication channel between
// nodes, through which they can submit, bid on and complete jobs.
type Transport interface {
	/////////////////////////////////////////////////////////////
	/// LIFECYCLE
	/////////////////////////////////////////////////////////////

	// Start the job scheduler. Not that this is blocking and can be managed
	// via the context parameter. You must call Subscribe _before_ starting.
	Start(ctx context.Context) error

	// Shuts down the transport layer and performs resource cleanup.
	Shutdown(ctx context.Context) error

	// HostID returns a unique string per host in whatever network the
	// scheduler is connecting to. Must be unique per instance.
	HostID(ctx context.Context) (string, error)

	/////////////////////////////////////////////////////////////
	/// EVENT HANDLING
	/////////////////////////////////////////////////////////////

	// This emits an event across the network to other nodes
	Publish(ctx context.Context, ev executor.JobEvent) error

	// Subscribe registers a callback for updates about any change to a job
	// or its results.  This is in-memory, global, singleton and scoped to the
	// lifetime of the process so no need for an unsubscribe right now.
	Subscribe(fn SubscribeFn)
}

// the data structure a client can use to render a view of the state of the world
// e.g. this is used to render the CLI table and results list
type ListResponse struct {
	Jobs map[string]executor.Job
}

// data structure for a Version response
type VersionResponse struct {
	VersionInfo executor.VersionInfo
}
