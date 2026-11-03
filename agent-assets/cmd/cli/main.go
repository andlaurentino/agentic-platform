// Command assets lists, validates and exports the asset catalog.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"agentic-platform/agent-assets/assets"
	"agentic-platform/agent-assets/sdk"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	catalog := assets.All()
	switch os.Args[1] {
	case "list":
		fs := flag.NewFlagSet("list", flag.ExitOnError)
		kind := fs.String("kind", "", "filter by kind: llm, tool, mcp, guardrail")
		_ = fs.Parse(os.Args[2:])
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tKIND\tLIFECYCLE\tOWNER\tNAME")
		for _, m := range catalog.Manifests(sdk.Kind(*kind)) {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", m.ID, m.Kind, m.Lifecycle, m.Owner, m.Name)
		}
		_ = w.Flush()
	case "validate":
		if err := catalog.Validate(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("ok: %d assets\n", len(catalog.Manifests("")))
	case "export":
		fs := flag.NewFlagSet("export", flag.ExitOnError)
		out := fs.String("o", "catalog/catalog.json", "output file, - for stdout")
		_ = fs.Parse(os.Args[2:])
		if err := catalog.Validate(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		b, err := json.MarshalIndent(catalog.Manifests(""), "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		b = append(b, '\n')
		if *out == "-" {
			_, _ = os.Stdout.Write(b)
			return
		}
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s\n", *out)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: assets <list [--kind K] | validate | export [-o file]>")
	os.Exit(2)
}
