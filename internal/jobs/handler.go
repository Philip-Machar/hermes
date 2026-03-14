package jobs

import (
	"context"
	"errors"
	"fmt"
)

// ErrUnknownJobType is returned when no handler is registered for a job type.
var ErrUnknownJobType = errors.New("unknown job type")

// Handler processes a single job. Returning a non-nil error signals failure
// and causes the worker to retry or send the job to the DLQ.
type Handler interface {
	Handle(ctx context.Context, job Job) error
}

// HandlerFunc is a convenience type that lets a plain function satisfy Handler.
type HandlerFunc func(ctx context.Context, job Job) error

func (f HandlerFunc) Handle(ctx context.Context, job Job) error {
	return f(ctx, job)
}

// HandlerRegistry maps job types to their Handler implementations.
// Register all handlers on startup; the worker looks up by job.Type at runtime.
type HandlerRegistry struct {
	handlers map[string]Handler
}

// NewHandlerRegistry returns an empty HandlerRegistry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{handlers: make(map[string]Handler)}
}

// Register associates a job type string with a Handler.
// Panics on duplicate registration to catch wiring mistakes at startup.
func (r *HandlerRegistry) Register(jobType string, h Handler) {
	if _, exists := r.handlers[jobType]; exists {
		panic(fmt.Sprintf("jobs: handler already registered for type %q", jobType))
	}
	r.handlers[jobType] = h
}

// Dispatch looks up the handler for job.Type and calls it.
// Returns ErrUnknownJobType if no handler is registered.
func (r *HandlerRegistry) Dispatch(ctx context.Context, job Job) error {
	h, ok := r.handlers[job.Type]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownJobType, job.Type)
	}
	return h.Handle(ctx, job)
}
