// Command assets lists, validates and exports the asset catalog.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"agentic-platform/agent-assets/assets"
	"agentic-platform/agent-assets/assets/core/llms"
	"agentic-platform/agent-assets/llmconfig"
	"agentic-platform/agent-assets/sdk"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "list":
		fs := flag.NewFlagSet("list", flag.ExitOnError)
		kind := fs.String("kind", "", "filter by kind: llm, tool, mcp, guardrail")
		catalog := catalogFromFlags(fs, args)
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tKIND\tLIFECYCLE\tOWNER\tNAME")
		for _, m := range catalog.Manifests(sdk.Kind(*kind)) {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", m.ID, m.Kind, m.Lifecycle, m.Owner, m.Name)
		}
		_ = w.Flush()
	case "llms":
		fs := flag.NewFlagSet("llms", flag.ExitOnError)
		catalog := catalogFromFlags(fs, args)
		printLLMs(catalog)
	case "validate":
		fs := flag.NewFlagSet("validate", flag.ExitOnError)
		catalog := catalogFromFlags(fs, args)
		if err := catalog.Validate(); err != nil {
			fail(err)
		}
		fmt.Printf("ok: %d assets, %d models\n", len(catalog.Manifests("")), len(catalog.ModelRefs()))
	case "export":
		// Always the embedded config without discovery, so the file is stable in CI.
		fs := flag.NewFlagSet("export", flag.ExitOnError)
		out := fs.String("o", "catalog/catalog.json", "output file, - for stdout")
		_ = fs.Parse(args)
		catalog := assets.All()
		if err := catalog.Validate(); err != nil {
			fail(err)
		}
		b, err := json.MarshalIndent(catalog.Manifests(""), "", "  ")
		if err != nil {
			fail(err)
		}
		b = append(b, '\n')
		if *out == "-" {
			_, _ = os.Stdout.Write(b)
			return
		}
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %s\n", *out)
	default:
		usage()
	}
}

// catalogFromFlags adds --llm-config and --discover to fs, parses args and builds the catalog.
func catalogFromFlags(fs *flag.FlagSet, args []string) *assets.Catalog {
	path := fs.String("llm-config", os.Getenv("LLM_CONFIG_PATH"), "LLM providers file (default: embedded llms.yaml)")
	discover := fs.Bool("discover", false, "list models from OpenAI-compatible endpoints that configure discover")
	_ = fs.Parse(args)
	cfg, err := llmconfig.Load(*path)
	if err != nil {
		fail(err)
	}
	return assets.New(context.Background(), assets.WithLLMConfig(cfg), assets.WithDiscovery(*discover))
}

func printLLMs(catalog *assets.Catalog) {
	for _, m := range catalog.Manifests(sdk.KindLLM) {
		a, _ := catalog.LLM(m.ID)
		p, ok := a.(*llms.Provider)
		if !ok {
			continue
		}
		cfg := p.Config()
		key := "none"
		if cfg.NeedsKey() {
			key = cfg.APIKeyEnv + " (not set)"
			if p.CredentialsConfigured() {
				key = cfg.APIKeyEnv + " (set)"
			}
		}
		d := p.Discovery()
		fmt.Printf("%s  [%s]  %s\n", m.ID, cfg.Type, m.Name)
		if cfg.BaseURL != "" {
			fmt.Printf("  endpoint:  %s\n", m.Extra["baseURL"])
		}
		fmt.Printf("  api key:   %s\n", key)
		discovery := d.Status
		if d.Error != "" {
			discovery += ": " + d.Error
		}
		fmt.Printf("  discovery: %s\n", discovery)
		var models []string
		for _, mi := range p.Models() {
			s := mi.ID
			if mi.Default {
				s += " (default)"
			}
			models = append(models, s)
		}
		fmt.Printf("  models:    %s\n\n", strings.Join(models, ", "))
	}
	if ref := catalog.DefaultModel(); ref != "" {
		fmt.Printf("guardrail default model: %s\n", ref)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: assets <command> [flags]

  list      [--kind K] [--llm-config file] [--discover]
  llms      [--llm-config file] [--discover]   show LLM providers and their models
  validate  [--llm-config file] [--discover]
  export    [-o file]                          write catalog/catalog.json (embedded config)`)
	os.Exit(2)
}
