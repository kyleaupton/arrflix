package email

import "fmt"

// Registry maps each Provider to its Builder. Copy of downloader.Registry.
type Registry struct {
	builders map[Provider]Builder
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{builders: map[Provider]Builder{}}
}

// Register registers a builder for a provider.
func (r *Registry) Register(p Provider, b Builder) {
	r.builders[p] = b
}

// Build builds a Transport from a config record, keyed on rec.Provider.
func (r *Registry) Build(rec ConfigRecord) (Transport, error) {
	b, ok := r.builders[rec.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown email provider: %s", rec.Provider)
	}
	return b(rec)
}
