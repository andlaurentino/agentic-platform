// Package assets aggregates every domain's assets into one catalog.
package assets

import (
	"agentic-platform/agent-assets/assets/core"
	"agentic-platform/agent-assets/internal/registry"
)

// Catalog is the registry type exposed to the platform.
type Catalog = registry.Registry

// All returns the catalog of every registered asset.
func All() *Catalog {
	r := registry.New()
	core.Register(r)
	return r
}
