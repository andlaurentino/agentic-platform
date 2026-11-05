package llmproviders

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"

	"agentic-platform/lib-agent-assets/pkg/assets"
	"agentic-platform/ms-ai-platform/internal/llmconfig"
)

const (
	refreshInterval = 60 * time.Second
	retryInterval   = 10 * time.Second
	listTimeout     = 10 * time.Second
)

// Refresh reads the endpoint's GET /models and reports whether the model list
// or status changed. On failure the last known list is kept.
func (p *Provider) Refresh(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	listed, err := p.listModels(ctx)
	prev := p.snap.Load()
	next := &snapshot{models: prev.models, status: Status{State: "ok", RefreshedAt: time.Now().UTC()}}
	if err != nil {
		next.status.State, next.status.Error = "failed", err.Error()
	} else {
		next.models = p.merge(listed)
	}
	next.status.Models = len(next.models)
	p.snap.Store(next)

	if prev.status.State != next.status.State {
		if err != nil {
			slog.Warn("LLM provider unavailable", "provider", p.cfg.Key, "url", displayURL(p.cfg.BaseURL), "err", err)
		} else {
			slog.Info("LLM provider available", "provider", p.cfg.Key, "models", len(next.models))
		}
	}
	return prev.status.State != next.status.State || prev.status.Error != next.status.Error || !slices.Equal(prev.models, next.models)
}

// listModels returns the endpoint's models, with the display_name and default
// extensions ms-llm-gateway adds.
func (p *Provider) listModels(ctx context.Context) ([]assets.ModelInfo, error) {
	key := ""
	if p.cfg.UsesKey() {
		key = os.Getenv(p.cfg.APIKeyEnv)
	}
	client := openai.NewClient(p.clientOptions(key)...)
	pager := client.Models.ListAutoPaging(ctx)
	var out []assets.ModelInfo
	for pager.Next() {
		m := pager.Current()
		info := assets.ModelInfo{ID: m.ID}
		if f, ok := m.JSON.ExtraFields["display_name"]; ok {
			_ = json.Unmarshal([]byte(f.Raw()), &info.Label)
		}
		if f, ok := m.JSON.ExtraFields["default"]; ok {
			_ = json.Unmarshal([]byte(f.Raw()), &info.Default)
		}
		out = append(out, info)
	}
	return out, pager.Err()
}

// merge filters listed models with include/exclude, sorts them, and applies
// labels and the default from the config.
func (p *Provider) merge(listed []assets.ModelInfo) []assets.ModelInfo {
	var out []assets.ModelInfo
	for _, m := range listed {
		if (len(p.cfg.Include) > 0 && !llmconfig.MatchAny(p.cfg.Include, m.ID)) || llmconfig.MatchAny(p.cfg.Exclude, m.ID) {
			continue
		}
		if i := slices.IndexFunc(p.cfg.Models, func(c llmconfig.Model) bool { return c.ID == m.ID }); i >= 0 {
			if c := p.cfg.Models[i]; c.Label != "" {
				m.Label = c.Label
			}
		}
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b assets.ModelInfo) int { return strings.Compare(a.ID, b.ID) })
	return slices.CompactFunc(out, func(a, b assets.ModelInfo) bool { return a.ID == b.ID })
}

// Refresher keeps every provider's model list fresh.
type Refresher struct {
	Providers []*Provider
	// OnChange runs (never concurrently) after a refresh changed a model list or status.
	OnChange func()
	mu       sync.Mutex
}

// RefreshAll refreshes every provider once, concurrently.
func (r *Refresher) RefreshAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range r.Providers {
		wg.Go(func() { p.Refresh(ctx) })
	}
	wg.Wait()
}

// Run refreshes each provider every minute, or every 10s while it is failing,
// until ctx ends.
func (r *Refresher) Run(ctx context.Context) {
	for _, p := range r.Providers {
		go func() {
			for {
				wait := refreshInterval
				if p.Status().State != "ok" {
					wait = retryInterval
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
				if p.Refresh(ctx) && r.OnChange != nil {
					r.mu.Lock()
					r.OnChange()
					r.mu.Unlock()
				}
			}
		}()
	}
}
