package tools

import (
	"fmt"
	"sync"

	"local-agent/internal/core"
)

// Registry stores tool specs and executors.
type Registry struct {
	mu        sync.RWMutex
	specs     map[string]core.ToolSpec
	executors map[string]Executor
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		specs:     map[string]core.ToolSpec{},
		executors: map[string]Executor{},
	}
}

// Register installs a spec/executor pair.
func (r *Registry) Register(spec core.ToolSpec, executor Executor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs[spec.Name] = spec
	r.executors[spec.Name] = executor
}

// Spec returns a tool spec by name.
func (r *Registry) Spec(name string) (core.ToolSpec, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.specs[name]
	if !ok {
		return core.ToolSpec{}, fmt.Errorf("tool spec not found: %s", name)
	}
	return spec, nil
}

// Executor returns an executor by name.
func (r *Registry) Executor(name string) (Executor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	executor, ok := r.executors[name]
	if !ok {
		return nil, fmt.Errorf("tool executor not found: %s", name)
	}
	return executor, nil
}

// List returns all registered specs.
func (r *Registry) List() []core.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]core.ToolSpec, 0, len(r.specs))
	for _, spec := range r.specs {
		items = append(items, spec)
	}
	return items
}
