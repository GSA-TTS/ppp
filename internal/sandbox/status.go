package sandbox

import "fmt"

// Status is a sandbox lifecycle state (spec §5.8).
type Status string

// The three lifecycle statuses defined by spec §5.8.
const (
	StatusCreated Status = "created"
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
)

// Valid reports whether s is one of the known lifecycle statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusCreated, StatusRunning, StatusStopped:
		return true
	default:
		return false
	}
}

// allowedTransitions is the exact permitted transition set:
//
//	created -> running   (first start after init)
//	running -> stopped   (stop a running sandbox)
//	stopped -> running   (restart a stopped sandbox)
//
// No transition targets created (it is an initial state only), and self-loops
// are rejected as no-ops.
var allowedTransitions = map[Status][]Status{
	StatusCreated: {StatusRunning},
	StatusRunning: {StatusStopped},
	StatusStopped: {StatusRunning},
}

// CanTransition reports whether moving from -> to is a permitted lifecycle
// transition.
func CanTransition(from, to Status) bool {
	for _, allowed := range allowedTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Transition moves the sandbox to the target status if the change is
// permitted, updating Status in place. On an invalid transition it returns an
// error and leaves Status unchanged.
func (s *Sandbox) Transition(to Status) error {
	if !to.Valid() {
		return fmt.Errorf("unknown target status %q", to)
	}
	if !CanTransition(s.Status, to) {
		return fmt.Errorf("invalid transition %q -> %q (allowed: created->running, running->stopped, stopped->running)", s.Status, to)
	}
	s.Status = to
	return nil
}
