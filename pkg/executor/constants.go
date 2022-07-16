package executor

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:generate stringer -type=EngineType --trimprefix=Engine
type EngineType int

const (
	engineUnknown EngineType = iota // must be first
	EngineNoop
	EngineDocker
	EngineWasm       // raw wasm executor not implemented yet
	EngineLanguage   // wraps python_wasm
	EnginePythonWasm // wraps docker
	engineDone       // must be last
)

func ParseEngineType(str string) (EngineType, error) {
	for typ := engineUnknown + 1; typ < engineDone; typ++ {
		if equal(typ.String(), str) {
			return typ, nil
		}
	}

	return engineUnknown, fmt.Errorf(
		"executor: unknown engine type '%s'", str)
}

func EngineTypes() []EngineType {
	var res []EngineType
	for typ := engineUnknown + 1; typ < engineDone; typ++ {
		res = append(res, typ)
	}

	return res
}

//go:generate stringer -type=JobEventType --trimprefix=JobEvent
type JobEventType int

const (
	jobEventUnknown JobEventType = iota // must be first
	JobEventCreated
	JobEventDealUpdated
	JobEventBid
	JobEventBidAccepted
	JobEventBidRejected
	JobEventResults
	JobEventResultsAccepted
	JobEventResultsRejected
	JobEventError
	jobEventDone // must be last
)

// IsTerminal returns true if the given event type signals the end of the
// lifecycle of a job. After this, all nodes can safely ignore the job.
func (event JobEventType) IsTerminal() bool {
	return event == JobEventError || event == JobEventResults
}

// IsIgnorable returns true if given event type signals that a node can safely
// ignore the rest of the job's lifecycle. This is the case for events caused
// by a node's bid being rejected.
func (event JobEventType) IsIgnorable() bool {
	return event.IsTerminal() || event == JobEventBidRejected
}

func ParseJobEventType(str string) (JobEventType, error) {
	for typ := jobEventUnknown + 1; typ < jobEventDone; typ++ {
		if equal(typ.String(), str) {
			return typ, nil
		}
	}

	return jobEventUnknown, fmt.Errorf(
		"executor: unknown job event type '%s'", str)
}

func JobEventTypes() []JobEventType {
	var res []JobEventType
	for typ := jobEventUnknown + 1; typ < jobEventDone; typ++ {
		res = append(res, typ)
	}

	return res
}

//go:generate stringer -type=JobStateType --trimprefix=JobState
// JobStateType is the state of a job on a particular node. Note that the job
// will typically have different states on different nodes.
type JobStateType int

const (
	jobStateUnknown JobStateType = iota // must be first
	JobStateBidding
	JobStateBidRejected
	JobStateRunning
	JobStateError
	JobStateComplete
	jobStateDone // must be last
)

// IsTerminal returns true if the given job type signals the end of the
// lifecycle of that job on a particular node. After this, the job can be
// safely ignored by the node.
func (typ JobStateType) IsTerminal() bool {
	return typ == JobStateComplete || typ == JobStateError || typ == JobStateBidRejected
}

// MarshalYAML encodes a JobStateType as a string for readability.
func (typ JobStateType) MarshalYAML() (interface{}, error) {
	return typ.String(), nil
}

// UnmarshalYAML decodes a JobStateType from a string or an int.
func (typ *JobStateType) UnmarshalYAML(value *yaml.Node) error {
	// First try and parse value.Value as an int:
	i, err := strconv.ParseInt(value.Value, 10, 32) // nolint:gomnd
	if err == nil {
		*typ = JobStateType(i)
		return nil
	}

	// If that fails, try to parse value.Value as a string:
	t, err := ParseJobStateType(value.Value)
	if err != nil {
		return err
	}

	*typ = t
	return nil
}

func ParseJobStateType(str string) (JobStateType, error) {
	for typ := jobStateUnknown + 1; typ < jobStateDone; typ++ {
		if equal(typ.String(), str) {
			return typ, nil
		}
	}

	return jobStateUnknown, fmt.Errorf(
		"executor: unknown job typ type '%s'", str)
}

func JobStateTypes() []JobStateType {
	var res []JobStateType
	for typ := jobStateUnknown + 1; typ < jobStateDone; typ++ {
		res = append(res, typ)
	}

	return res
}

func equal(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	return strings.EqualFold(a, b)
}
