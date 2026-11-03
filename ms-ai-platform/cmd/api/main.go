// Command api runs the platform REST API and the AG-UI agent runtime.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/adk/session"

	"agentic-platform/agent-assets/assets"
	"agentic-platform/agent-assets/llmconfig"
	"agentic-platform/ms-ai-platform/internal/agui"
	"agentic-platform/ms-ai-platform/internal/catalog"
	"agentic-platform/ms-ai-platform/internal/compiler"
	"agentic-platform/ms-ai-platform/internal/httpx"
	"agentic-platform/ms-ai-platform/internal/orchestrations"
	"agentic-platform/ms-ai-platform/internal/secrets"
	"agentic-platform/ms-ai-platform/internal/skills"
	"agentic-platform/ms-ai-platform/internal/spec"
	"agentic-platform/ms-ai-platform/internal/store"
)

func main() {
	if err := run(); err != nil {
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

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	llmCfg, err := llmconfig.Load(os.Getenv("LLM_CONFIG_PATH"))
	if err != nil {
		return err
	}
	registry := assets.New(ctx, assets.WithLLMConfig(llmCfg), assets.WithDiscovery(true))
	if err := registry.Validate(); err != nil {
		return err
	}
	cat := catalog.New(registry, secrets.Env{})
	for _, p := range cat.Providers() {
		log := slog.Info
		if !p.CredentialsConfigured || p.Models == 0 {
			log = slog.Warn
		}
		log("LLM provider", "id", p.ID, "type", p.Type, "models", p.Models, "credentials", p.CredentialsConfigured, "discovery", p.Discovery)
	}

	st, err := store.Open(ctx, env("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/agentic?sslmode=disable"))
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.SyncCatalog(ctx, registry.Manifests("")); err != nil {
		return err
	}

	validator := spec.NewValidator(registry)
	mux := http.NewServeMux()
	cat.Routes(mux)
	(&skills.Handler{Store: st}).Routes(mux)
	(&orchestrations.Handler{Store: st, Validator: validator}).Routes(mux)
	mux.Handle("POST /v1/orchestrations/{id}/agui", &agui.Handler{
		Source:   st,
		Compiler: &compiler.Compiler{Catalog: cat, Validator: validator},
		Sessions: session.InMemoryService(),
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		dbOK := st.Ping(r.Context()) == nil
		status := http.StatusOK
		if !dbOK {
			status = http.StatusServiceUnavailable
		}
		httpx.WriteJSON(w, status, map[string]any{
			"status":       map[bool]string{true: "ok", false: "degraded"}[dbOK],
			"database":     dbOK,
			"assets":       len(registry.Manifests("")),
			"llmProviders": cat.Providers(),
		})
	})

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           httpx.Logging(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("ms-ai-platform listening", "addr", srv.Addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
