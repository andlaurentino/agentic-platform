// Package assets aggregates every domain's assets into one catalog.
package assets

import (
	"agentic-platform/lib-agent-assets/assets/core"
	"agentic-platform/lib-agent-assets/pkg/registry"
)

// New builds the catalog with every domain's assets. LLM providers are not
// part of lib-agent-assets: the platform registers the LLM gateways it
// connects to with MustLLM.
func New() *registry.Registry {
	r := registry.New()
	core.Register(r)
	return r
}
