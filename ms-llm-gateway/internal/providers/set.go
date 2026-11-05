package providers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"agentic-platform/ms-llm-gateway/internal/llmconfig"
	"agentic-platform/ms-llm-gateway/internal/wire"
)

// Set is every configured provider, ordered by key.
type Set struct {
	providers    []*Provider
	byKey        map[string]*Provider
	defaultModel string
	created      int64
}

func NewSet(cfg llmconfig.Config) *Set {
	s := &Set{byKey: map[string]*Provider{}, defaultModel: cfg.Defaults.Model, created: time.Now().Unix()}
	for _, pc := range cfg.Sorted() {
		p := NewProvider(pc)
		s.providers = append(s.providers, p)
		s.byKey[pc.Key] = p
	}
	return s
}

func (s *Set) Providers() []*Provider { return s.providers }

// Resolve finds the provider and model for a gateway model id. "<provider>/<model>"
// selects a provider; any other id goes to the first provider that offers it,
// so upstream ids that contain "/" themselves still work.
func (s *Set) Resolve(id string) (*Provider, llmconfig.Model, *wire.Error) {
	if key, model, ok := strings.Cut(id, "/"); ok {
		if p, known := s.byKey[key]; known {
			if m, found := p.Model(model); found {
				return p, m, nil
			}
			return nil, llmconfig.Model{}, wire.Errorf(http.StatusNotFound, "invalid_request_error", "model_not_found",
				"model %q is not available on %s", model, p.Cfg.Name)
		}
	}
	for _, p := range s.providers {
		if m, ok := p.Model(id); ok {
			return p, m, nil
		}
	}
	return nil, llmconfig.Model{}, wire.Errorf(http.StatusNotFound, "invalid_request_error", "model_not_found",
		"model %q does not exist; GET /v1/models lists the available models", id)
}

// List returns every model as a GET /v1/models entry.
func (s *Set) List() []wire.Model {
	out := []wire.Model{}
	for _, p := range s.providers {
		for _, m := range p.Models() {
			id := llmconfig.ModelID(p.Cfg.Key, m.ID)
			label := m.Label
			if label == "" {
				label = m.ID
			}
			out = append(out, wire.Model{
				ID: id, Object: "model", Created: s.created, OwnedBy: p.Cfg.Key,
				DisplayName: p.Cfg.Name + " · " + label, Default: id == s.defaultModel,
			})
		}
	}
	return out
}

// DiscoverAll runs discovery once for every provider, concurrently.
func (s *Set) DiscoverAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range s.providers {
		wg.Go(func() { p.Discover(ctx) })
	}
	wg.Wait()
}

// RunDiscovery refreshes discovered model lists until ctx ends.
func (s *Set) RunDiscovery(ctx context.Context) {
	for _, p := range s.providers {
		if p.Cfg.Discover == nil {
			continue
		}
		go func() {
			t := time.NewTicker(p.Cfg.RefreshInterval())
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					p.Discover(ctx)
				}
			}
		}()
	}
}

// Check reports configuration problems that only show at runtime.
func (s *Set) Check() []error {
	var errs []error
	for _, p := range s.providers {
		if !p.Cfg.CredentialsConfigured() {
			errs = append(errs, fmt.Errorf("%s: %s is not set", p.Cfg.Key, p.Cfg.APIKeyEnv))
		}
		if d := p.Discovery(); d.Status == "failed" {
			errs = append(errs, fmt.Errorf("%s: discovery failed: %s", p.Cfg.Key, d.Error))
		}
		if len(p.Models()) == 0 {
			errs = append(errs, fmt.Errorf("%s: no models available", p.Cfg.Key))
		}
	}
	return errs
}
