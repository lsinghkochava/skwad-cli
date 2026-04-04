package pipeline

import (
	"errors"
	"fmt"
	"time"
)

var ErrMaxIterationsReached = errors.New("max iterations reached")

// PipelineEvent records a phase/iteration lifecycle event.
type PipelineEvent struct {
	Time      time.Time `json:"time"`
	Type      string    `json:"type"`      // "phase_start", "phase_end", "iteration", "timeout", "complete"
	Phase     string    `json:"phase,omitempty"`
	Iteration int       `json:"iteration"`
	Detail    string    `json:"detail,omitempty"`
}

// Pipeline tracks iteration state and phase transitions for a CI run.
type Pipeline struct {
	MaxIterations int
	Timeout       time.Duration
	Iteration     int
	Phase         string
	StartedAt     time.Time
	Events        []PipelineEvent
}

// NewPipeline creates a pipeline with iteration limits and timeout.
func NewPipeline(maxIterations int, timeout time.Duration) *Pipeline {
	return &Pipeline{
		MaxIterations: maxIterations,
		Timeout:       timeout,
		StartedAt:     time.Now(),
	}
}

// NextIteration increments the iteration counter. Returns the new iteration
// number or ErrMaxIterationsReached if the limit has been hit.
func (p *Pipeline) NextIteration() (int, error) {
	if p.MaxIterations > 0 && p.Iteration >= p.MaxIterations {
		p.RecordEvent("max_iterations", fmt.Sprintf("limit %d reached", p.MaxIterations))
		return p.Iteration, ErrMaxIterationsReached
	}
	p.Iteration++
	p.RecordEvent("iteration", fmt.Sprintf("iteration %d started", p.Iteration))
	return p.Iteration, nil
}

// SetPhase transitions to a new phase.
func (p *Pipeline) SetPhase(name string) {
	if p.Phase != "" {
		p.RecordEvent("phase_end", p.Phase)
	}
	p.Phase = name
	p.RecordEvent("phase_start", name)
}

// RecordEvent appends an event to the pipeline log.
func (p *Pipeline) RecordEvent(eventType, detail string) {
	p.Events = append(p.Events, PipelineEvent{
		Time:      time.Now(),
		Type:      eventType,
		Phase:     p.Phase,
		Iteration: p.Iteration,
		Detail:    detail,
	})
}

// IsExpired returns true if the pipeline has exceeded its timeout.
func (p *Pipeline) IsExpired() bool {
	if p.Timeout <= 0 {
		return false
	}
	return time.Since(p.StartedAt) > p.Timeout
}
