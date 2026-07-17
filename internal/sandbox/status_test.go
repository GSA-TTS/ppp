package sandbox_test

import (
	"testing"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// TestTransition covers the full status transition matrix. Allowed set
// (spec §5.8 statuses created/running/stopped):
//
//	created -> running
//	running -> stopped
//	stopped -> running
//
// Everything else (including any -> created and same-state self-loops) is
// rejected.
func TestTransition(t *testing.T) {
	cases := []struct {
		name string
		from sandbox.Status
		to   sandbox.Status
		ok   bool
	}{
		{"created->running", sandbox.StatusCreated, sandbox.StatusRunning, true},
		{"running->stopped", sandbox.StatusRunning, sandbox.StatusStopped, true},
		{"stopped->running", sandbox.StatusStopped, sandbox.StatusRunning, true},
		{"created->stopped invalid", sandbox.StatusCreated, sandbox.StatusStopped, false},
		{"running->created invalid", sandbox.StatusRunning, sandbox.StatusCreated, false},
		{"stopped->created invalid", sandbox.StatusStopped, sandbox.StatusCreated, false},
		{"created->created invalid", sandbox.StatusCreated, sandbox.StatusCreated, false},
		{"running->running invalid", sandbox.StatusRunning, sandbox.StatusRunning, false},
		{"stopped->stopped invalid", sandbox.StatusStopped, sandbox.StatusStopped, false},
		{"unknown target invalid", sandbox.StatusCreated, sandbox.Status("bogus"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := sandbox.Sandbox{Name: "x", MachineName: "x", Status: tc.from}
			err := s.Transition(tc.to)
			if tc.ok && err != nil {
				t.Fatalf("Transition(%s->%s): unexpected error %v", tc.from, tc.to, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("Transition(%s->%s): want error, got nil", tc.from, tc.to)
			}
			if tc.ok {
				if s.Status != tc.to {
					t.Errorf("status = %s, want %s after allowed transition", s.Status, tc.to)
				}
			} else {
				if s.Status != tc.from {
					t.Errorf("status = %s, want unchanged %s after rejected transition", s.Status, tc.from)
				}
			}
		})
	}
}

func TestStatusValid(t *testing.T) {
	cases := []struct {
		status sandbox.Status
		valid  bool
	}{
		{sandbox.StatusCreated, true},
		{sandbox.StatusRunning, true},
		{sandbox.StatusStopped, true},
		{sandbox.Status(""), false},
		{sandbox.Status("paused"), false},
	}
	for _, tc := range cases {
		name := string(tc.status)
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			if got := tc.status.Valid(); got != tc.valid {
				t.Errorf("Status(%q).Valid() = %v, want %v", tc.status, got, tc.valid)
			}
		})
	}
}
