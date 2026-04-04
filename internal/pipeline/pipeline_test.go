package pipeline

import (
	"errors"
	"testing"
	"time"
)

func TestNextIteration_CountsUpToMax(t *testing.T) {
	p := NewPipeline(3, 0)
	for i := 1; i <= 3; i++ {
		n, err := p.NextIteration()
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if n != i {
			t.Fatalf("expected iteration %d, got %d", i, n)
		}
	}
}

func TestNextIteration_ErrorOnExceed(t *testing.T) {
	p := NewPipeline(3, 0)
	for i := 0; i < 3; i++ {
		if _, err := p.NextIteration(); err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i+1, err)
		}
	}
	_, err := p.NextIteration()
	if !errors.Is(err, ErrMaxIterationsReached) {
		t.Fatalf("expected ErrMaxIterationsReached, got %v", err)
	}
}

func TestSetPhase_RecordsEvents(t *testing.T) {
	p := NewPipeline(10, 0)
	p.SetPhase("build")
	p.SetPhase("test")

	// Expect: phase_start(build), phase_end(build), phase_start(test)
	if len(p.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(p.Events))
	}
	if p.Events[0].Type != "phase_start" || p.Events[0].Detail != "build" {
		t.Errorf("event[0]: expected phase_start/build, got %s/%s", p.Events[0].Type, p.Events[0].Detail)
	}
	if p.Events[1].Type != "phase_end" || p.Events[1].Detail != "build" {
		t.Errorf("event[1]: expected phase_end/build, got %s/%s", p.Events[1].Type, p.Events[1].Detail)
	}
	if p.Events[2].Type != "phase_start" || p.Events[2].Detail != "test" {
		t.Errorf("event[2]: expected phase_start/test, got %s/%s", p.Events[2].Type, p.Events[2].Detail)
	}
}

func TestIsExpired(t *testing.T) {
	p := NewPipeline(10, 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if !p.IsExpired() {
		t.Error("expected pipeline to be expired")
	}
}

func TestIsExpired_NoTimeout(t *testing.T) {
	p := NewPipeline(10, 0)
	if p.IsExpired() {
		t.Error("expected pipeline with zero timeout to never expire")
	}
}

func TestUnlimitedIterations(t *testing.T) {
	p := NewPipeline(0, 0)
	for i := 0; i < 100; i++ {
		if _, err := p.NextIteration(); err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i+1, err)
		}
	}
	if p.Iteration != 100 {
		t.Fatalf("expected 100 iterations, got %d", p.Iteration)
	}
}

func TestEventsAppendedInOrder(t *testing.T) {
	p := NewPipeline(5, 0)
	p.SetPhase("init")
	p.NextIteration()
	p.NextIteration()
	p.SetPhase("run")

	// Verify timestamps are non-decreasing.
	for i := 1; i < len(p.Events); i++ {
		if p.Events[i].Time.Before(p.Events[i-1].Time) {
			t.Errorf("event[%d] timestamp before event[%d]", i, i-1)
		}
	}
}

func TestSetPhase_FirstPhase_NoPhaseEnd(t *testing.T) {
	p := NewPipeline(5, 0)
	p.SetPhase("build")

	// First phase should only produce phase_start, no phase_end
	if len(p.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(p.Events))
	}
	if p.Events[0].Type != "phase_start" {
		t.Errorf("expected phase_start, got %s", p.Events[0].Type)
	}
	if p.Phase != "build" {
		t.Errorf("expected Phase=build, got %s", p.Phase)
	}
}

func TestRecordEvent_DirectCall(t *testing.T) {
	p := NewPipeline(5, 0)
	p.RecordEvent("custom", "something happened")

	if len(p.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(p.Events))
	}
	if p.Events[0].Type != "custom" {
		t.Errorf("expected type=custom, got %s", p.Events[0].Type)
	}
	if p.Events[0].Detail != "something happened" {
		t.Errorf("expected detail='something happened', got %q", p.Events[0].Detail)
	}
}

func TestNextIteration_ExceedRecordsMaxIterationsEvent(t *testing.T) {
	p := NewPipeline(1, 0)
	p.NextIteration() // iteration 1 - succeeds

	_, err := p.NextIteration() // iteration 2 - fails
	if !errors.Is(err, ErrMaxIterationsReached) {
		t.Fatalf("expected ErrMaxIterationsReached, got %v", err)
	}

	// Should have recorded a max_iterations event
	found := false
	for _, e := range p.Events {
		if e.Type == "max_iterations" {
			found = true
			if e.Detail != "limit 1 reached" {
				t.Errorf("expected detail 'limit 1 reached', got %q", e.Detail)
			}
		}
	}
	if !found {
		t.Error("expected max_iterations event to be recorded")
	}
}

func TestNextIteration_EventDetail(t *testing.T) {
	p := NewPipeline(5, 0)
	p.NextIteration()

	found := false
	for _, e := range p.Events {
		if e.Type == "iteration" && e.Detail == "iteration 1 started" {
			found = true
		}
	}
	if !found {
		t.Error("expected iteration event with detail 'iteration 1 started'")
	}
}

func TestPipeline_PhaseTrackedInEvents(t *testing.T) {
	p := NewPipeline(5, 0)
	p.SetPhase("build")
	p.NextIteration()

	// The iteration event should have Phase="build"
	for _, e := range p.Events {
		if e.Type == "iteration" {
			if e.Phase != "build" {
				t.Errorf("expected iteration event to have Phase=build, got %q", e.Phase)
			}
		}
	}
}

func TestNewPipeline_InitialState(t *testing.T) {
	p := NewPipeline(3, 10*time.Second)
	if p.MaxIterations != 3 {
		t.Errorf("expected MaxIterations=3, got %d", p.MaxIterations)
	}
	if p.Timeout != 10*time.Second {
		t.Errorf("expected Timeout=10s, got %v", p.Timeout)
	}
	if p.Iteration != 0 {
		t.Errorf("expected Iteration=0, got %d", p.Iteration)
	}
	if p.Phase != "" {
		t.Errorf("expected empty Phase, got %q", p.Phase)
	}
	if len(p.Events) != 0 {
		t.Errorf("expected no events, got %d", len(p.Events))
	}
	if p.StartedAt.IsZero() {
		t.Error("expected non-zero StartedAt")
	}
}
