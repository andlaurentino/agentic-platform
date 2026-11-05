// Command assets lists, validates and exports the asset catalog.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	allassets "agentic-platform/lib-agent-assets/assets"
	"agentic-platform/lib-agent-assets/pkg/assets"
	"agentic-platform/lib-agent-assets/pkg/registry"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "list":
		fs := flag.NewFlagSet("list", flag.ExitOnError)
		kind := fs.String("kind", "", "filter by kind: tool, mcp, guardrail")
		catalog := catalogFromFlags(fs, args)
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tKIND\tLIFECYCLE\tOWNER\tNAME")
		for _, m := range catalog.Manifests(assets.Kind(*kind)) {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", m.ID, m.Kind, m.Lifecycle, m.Owner, m.Name)
		}
		_ = w.Flush()
	case "validate":
		fs := flag.NewFlagSet("validate", flag.ExitOnError)
		catalog := catalogFromFlags(fs, args)
		if err := catalog.Validate(); err != nil {
			fail(err)
		}
		fmt.Printf("ok: %d assets\n", len(catalog.Manifests("")))
	case "export":
		fs := flag.NewFlagSet("export", flag.ExitOnError)
		out := fs.String("o", "-", "output file, - for stdout")
		_ = fs.Parse(args)
		catalog := allassets.New()
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

// catalogFromFlags parses args and builds the catalog.
func catalogFromFlags(fs *flag.FlagSet, args []string) *registry.Registry {
	_ = fs.Parse(args)
	return allassets.New()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: assets <command> [flags]

  list      [--kind K]
  validate
  export    [-o file]   print every manifest as JSON (or write it to a file)

LLM providers are not assets: they are configured in ms-llm-gateway (llms.yaml)
and registered by ms-ai-platform (llm-providers.yaml).`)
	os.Exit(2)
}
