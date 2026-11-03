package llms

import (
	"context"
	"os"
	"path"
	"slices"

	"github.com/openai/openai-go/v3"

	"agentic-platform/agent-assets/llmconfig"
)

// discoverModels lists model ids from GET {baseURL}/models.
func discoverModels(ctx context.Context, cfg llmconfig.Provider) ([]string, error) {
	key := ""
	if cfg.NeedsKey() {
		key = os.Getenv(cfg.APIKeyEnv)
	}
	client := openai.NewClient(openaiOptions(cfg, key, cfg.ExpandedHeaders())...)
	pager := client.Models.ListAutoPaging(ctx)
	var ids []string
	for pager.Next() {
		ids = append(ids, pager.Current().ID)
	}
	return ids, pager.Err()
}

// filterModels keeps ids matching any include glob (all, if none) and no exclude glob.
func filterModels(ids []string, d llmconfig.Discover) []string {
	match := func(globs []string, id string) bool {
		return slices.ContainsFunc(globs, func(g string) bool { ok, _ := path.Match(g, id); return ok })
	}
	var out []string
	for _, id := range ids {
		if (len(d.Include) == 0 || match(d.Include, id)) && !match(d.Exclude, id) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// mergeModels returns the discovered ids, using labels and flags from configured
// models when ids match. Configured models the endpoint did not return are dropped,
// since calls to them would fail.
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
