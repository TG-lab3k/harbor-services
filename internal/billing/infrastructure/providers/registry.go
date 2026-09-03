package providers

import (
	"sync"

	"github.com/okok/harbor-services/internal/billing/domain"
)

// Registry maps provider name → adapter.
type Registry struct {
	mu   sync.RWMutex
	byName map[string]domain.Provider
}

func NewRegistry(providers ...domain.Provider) *Registry {
	r := &Registry{byName: make(map[string]domain.Provider)}
	for _, p := range providers {
		r.Register(p)
	}
	return r
}

func (r *Registry) Register(p domain.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName[p.Name()] = p
}

func (r *Registry) Get(name string) (domain.Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byName[name]
	return p, ok
}
