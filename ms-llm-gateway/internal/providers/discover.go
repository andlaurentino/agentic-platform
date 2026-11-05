package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"agentic-platform/ms-llm-gateway/internal/llmconfig"
)

// listModels returns model ids from GET {baseURL}/models.
func (p *Provider) listModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(p.Cfg.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	p.setOpenAIHeaders(req)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /models: %d %s", resp.StatusCode, upstreamMessage(body))
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("GET /models: %w", err)
	}
	ids := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// filterModels keeps ids matching any include glob (all, if none) and no exclude glob.
func filterModels(ids []string, d llmconfig.Discover) []string {
	var out []string
	for _, id := range ids {
		if (len(d.Include) == 0 || llmconfig.MatchAny(d.Include, id)) && !llmconfig.MatchAny(d.Exclude, id) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// mergeModels returns the discovered ids, using labels and flags from configured
// models when ids match. Configured models the upstream did not return are
// dropped, since calls to them would fail.
func mergeModels(configured []llmconfig.Model, discovered []string) []llmconfig.Model {
	out := make([]llmconfig.Model, 0, len(discovered))
	for _, id := range discovered {
		m := llmconfig.Model{ID: id}
		if i := slices.IndexFunc(configured, func(c llmconfig.Model) bool { return c.ID == id }); i >= 0 {
			m = configured[i]
		}
		out = append(out, m)
	}
	return out
}
