// Package cancelregistry tracks per-job cancel functions so that an external
// signal (e.g. from a Redis cancel queue) can abort an in-flight job.
package cancelregistry

import (
	"context"
	"sync"
)

// Registry is a thread-safe map of job ID → context.CancelFunc.
// Register a cancel func before starting work; call Cancel to abort it;
// call Unregister when the job finishes (success or failure).
type Registry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{cancels: make(map[string]context.CancelFunc)}
}

// Register stores cancel for the given jobID, replacing any previous entry.
func (r *Registry) Register(jobID string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[jobID] = cancel
}

// Cancel calls the cancel func for jobID if one is registered; no-op otherwise.
func (r *Registry) Cancel(jobID string) {
	r.mu.Lock()
	cancel, ok := r.cancels[jobID]
	r.mu.Unlock()
	if ok {
		cancel()
	}
}

// Unregister removes the entry for jobID from the registry.
func (r *Registry) Unregister(jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, jobID)
}
