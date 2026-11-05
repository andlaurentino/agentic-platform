// Command gateway runs ms-llm-gateway: an OpenAI-compatible /v1 API in front of
// the LLM providers configured in llms.yaml.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"agentic-platform/ms-llm-gateway/internal/llmconfig"
	"agentic-platform/ms-llm-gateway/internal/providers"
	"agentic-platform/ms-llm-gateway/internal/server"
)

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "check" {
		err = check(os.Args[2:])
	} else if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, `usage: gateway             serve the API (GATEWAY_ADDR, LLM_CONFIG_PATH, GATEWAY_API_KEYS)
       gateway check [--config file] [--discover]   validate a config and show its models`)
		os.Exit(2)
	} else {
		err = serve()
	}
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := llmconfig.Load(os.Getenv("LLM_CONFIG_PATH"))
	if err != nil {
		return err
	}
	set := providers.NewSet(cfg)
	set.DiscoverAll(ctx)
	set.RunDiscovery(ctx)
	srv := &server.Server{Providers: set, APIKeys: server.ParseKeys(os.Getenv("GATEWAY_API_KEYS"))}
	for _, h := range srv.Health() {
		log := slog.Info
		if !h.CredentialsConfigured || h.Models == 0 {
			log = slog.Warn
		}
		log("LLM provider", "key", h.Key, "type", h.Type, "models", h.Models, "credentials", h.CredentialsConfigured, "discovery", h.Discovery.Status)
	}
	if len(srv.APIKeys) == 0 {
		slog.Warn("GATEWAY_API_KEYS is not set: the gateway accepts requests without a key; keep it on the internal network")
	}

	httpSrv := &http.Server{
		Addr:              env("GATEWAY_ADDR", ":8082"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("ms-llm-gateway listening", "addr", httpSrv.Addr)
		errc <- httpSrv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// check validates a config and prints its providers and models.
func check(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	path := fs.String("config", os.Getenv("LLM_CONFIG_PATH"), "llms.yaml to check (default: the embedded file)")
	discover := fs.Bool("discover", false, "list models from upstreams that configure discover")
	_ = fs.Parse(args)
	cfg, err := llmconfig.Load(*path)
	if err != nil {
		return err
	}
	set := providers.NewSet(cfg)
	if *discover {
		set.DiscoverAll(context.Background())
	}
	for _, p := range set.Providers() {
		key := "none"
		if p.Cfg.NeedsKey() {
			key = p.Cfg.APIKeyEnv + " (not set)"
			if p.Cfg.CredentialsConfigured() {
				key = p.Cfg.APIKeyEnv + " (set)"
			}
		}
		fmt.Printf("%s  [%s]  %s\n", p.Cfg.Key, p.Cfg.Type, p.Cfg.Name)
		if p.Cfg.BaseURL != "" {
			fmt.Printf("  endpoint:  %s\n", p.Cfg.BaseURL)
		}
		fmt.Printf("  api key:   %s\n", key)
		if d := p.Discovery(); d.Status != "off" || p.Cfg.Discover != nil {
			status := d.Status
			if !*discover {
				status = "not run (pass --discover)"
			} else if d.Error != "" {
				status += ": " + d.Error
			}
			fmt.Printf("  discovery: %s\n", status)
		}
		fmt.Println("  models:")
		for _, m := range p.Models() {
			fmt.Printf("    %s/%s\n", p.Cfg.Key, m.ID)
		}
		fmt.Println()
	}
	if cfg.Defaults.Model != "" {
		fmt.Printf("default model: %s\n", cfg.Defaults.Model)
	}
	if *discover {
		if errs := set.Check(); len(errs) > 0 {
			var msgs []string
			for _, e := range errs {
				msgs = append(msgs, e.Error())
			}
			return errors.New("problems:\n  " + strings.Join(msgs, "\n  "))
		}
	}
	return nil
}
