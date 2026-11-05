package spec

import (
	"encoding/json"
	"strings"
)

// DefaultLLM is the LLM asset new agents use: the platform's ms-llm-gateway.
const DefaultLLM = "core.llm.gateway"

// UpgradeLegacyLLMs rewrites specs saved when LLM providers were agent assets
// (core.llm.<provider> with that provider's model id) to the default LLM
// gateway, which serves the same model as "<provider>/<model>". Guardrail
// model references "<provider>:<model>" move the same way. Only references
// to providers that are no longer registered are changed. It reports whether
// anything changed.
func UpgradeLegacyLLMs(o *Orchestration, registered func(assetID string) bool) bool {
	if !registered(DefaultLLM) {
		return false
	}
	gateway := strings.TrimPrefix(DefaultLLM, "core.llm.")
	changed := false
	for i := range o.Agents {
		a := &o.Agents[i]
		if key, ok := strings.CutPrefix(a.Model.Asset, "core.llm."); ok && !registered(a.Model.Asset) {
			cfg := decode(a.Model.Config)
			if m, _ := cfg["model"].(string); m != "" {
				cfg["model"] = key + "/" + m
			}
			if effort, ok := cfg["reasoningEffort"]; ok {
				if _, has := cfg["effort"]; !has {
					cfg["effort"] = effort
				}
				delete(cfg, "reasoningEffort")
			}
			a.Model = AssetRef{Asset: DefaultLLM, Config: encode(cfg)}
			changed = true
		}
		for j := range a.Guardrails {
			changed = upgradeModelRef(&a.Guardrails[j], gateway, registered) || changed
		}
	}
	for i := range o.Guardrails {
		changed = upgradeModelRef(&o.Guardrails[i], gateway, registered) || changed
	}
	return changed
}

func upgradeModelRef(g *AssetRef, gateway string, registered func(string) bool) bool {
	cfg := decode(g.Config)
	ref, _ := cfg["model"].(string)
	provider, model, ok := strings.Cut(ref, ":")
	if !ok || registered("core.llm."+provider) {
		return false
	}
	cfg["model"] = gateway + ":" + provider + "/" + model
	g.Config = encode(cfg)
	return true
}

func decode(raw json.RawMessage) map[string]any {
	cfg := map[string]any{}
	_ = json.Unmarshal(raw, &cfg)
	return cfg
}

func encode(cfg map[string]any) json.RawMessage {
	b, _ := json.Marshal(cfg)
	return b
}
