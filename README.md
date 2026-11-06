# Agentic Platform

Internal platform for building conversational AI agents. Engineers register **assets** (tools, MCP servers, guardrails) in code and connect LLM providers to an **LLM gateway**; builders combine them into agents and orchestrations in a visual editor and test them in a playground. See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design.

This repo currently contains **slice 0**: a thin, working path through every service.

| Folder | What it is |
|---|---|
| `lib-agent-assets/` | Go module: the asset library (`pkg/assets`, `pkg/registry`), the core assets (tools, MCPs, guardrails) and the `assets` CLI |
| `ms-ai-platform/` | Go API: catalog, LLM gateway connections, skills, orchestrations + draft, spec → Google ADK compiler, AG-UI runtime |
| `ms-llm-gateway/` | Go service: OpenAI-compatible `/v1` API in front of the LLM providers (Anthropic, OpenAI-compatible upstreams) |
| `web-ai-platform/` | React + Tailwind + shadcn app: sidebar, catalog, skills, canvas editor, playground |
| `demo-mcp/` | Example MCP server (fake weather, orders, FAQ) |

## Run it locally

Requirements: Go 1.27+, Node 24+, Docker (for Postgres), an Anthropic API key with credits.

```bash
cp .env.example .env        # set ANTHROPIC_API_KEY
make up                     # Postgres + every service, with live reload → http://localhost:5173
```

`make up` starts Postgres in Docker (:5433), then runs the services listed in [`dev.yaml`](dev.yaml) with one colored log stream:

| Service | Port | Reloads when |
|---|---|---|
| `gateway` (ms-llm-gateway) | 8082 | `ms-llm-gateway/**/*.go`, `*.yaml` change: rebuild + restart |
| `mcp` (demo-mcp) | 8081 | `demo-mcp/**/*.go` change |
| `api` (ms-ai-platform) | 8080 | `ms-ai-platform/**` or `lib-agent-assets/**` (`.go`, `.yaml`, `.sql`) change |
| `web` (Vite) | 5173 | Vite hot-reloads the browser itself |

- A build that fails keeps the previous version running; fix the code and save to retry.
- `Ctrl+C` stops all services; Postgres keeps running (`make down` stops it).
- Run only some services: `go run ./tools/devrun api web`. Ports already in use are reported before anything starts.
- `.env` is loaded for every service. Each service can still be run alone with `make dev-gateway`, `dev-mcp`, `dev-api`, `dev-web`.

Then: **Orchestrations → New orchestration**, right-click the canvas to add a new agent or an existing one from the agent library, drag from an agent's bottom handle to another agent to delegate (click an arrow to edit the connection, right-click it or an agent for more options), pick tools/MCP tools/guardrails/skills in the **Agent** tab, set guardrails for every agent in **Settings**, and chat in **Test**. Agents are shared: the **Agents** page lists the library, and editing an agent changes it in every orchestration that uses it.

Try the AG-UI endpoint without the UI:

```bash
curl -N -X POST "localhost:8080/v1/orchestrations/<id>/agui?target=draft" \
  -d '{"threadId":"t1","runId":"r1","messages":[{"id":"m1","role":"user","content":"Weather in Lisbon?"}]}'
```

Start order doesn't matter: the platform checks each LLM gateway every 10 seconds until it answers, then every minute, so models appear without a restart.

`ANTHROPIC_BASE_URL` points the gateway's Anthropic providers that don't set `baseURL` at another endpoint (for example a local fake) if you need to run without credits.

## Configuring LLMs

LLMs are configured in two places:

```
ms-ai-platform ──OpenAI /v1──▶ ms-llm-gateway ──▶ Anthropic, OpenAI-compatible upstreams
   llm-providers.yaml            llms.yaml + provider keys
```

### The gateway: `ms-llm-gateway/config/llms.yaml`

Upstream providers and their credentials. The file is embedded in the gateway binary; `LLM_CONFIG_PATH=/path/to/llms.yaml` uses another one. Every model is served as `<provider>/<model>`.

```yaml
defaults:
  model: claude-haiku/claude-haiku-5-5   # marked default in GET /v1/models

providers:
  claude-haiku:
    type: anthropic                    # Anthropic Messages API
    apiKeyEnv: ANTHROPIC_API_KEY       # name of the env var, never the key itself
    models:
      - { id: claude-haiku-5-5, label: Claude Haiku 5.5, default: true, effort: true }
      - { id: claude-haiku-4-5, label: Claude Haiku 4.5 }
    params: { maxTokens: 4096, effort: low }

  company-gateway:
    name: Company Gateway
    type: openai                       # OpenAI Chat Completions: OpenAI, LiteLLM, vLLM, ...
    baseURL: https://llm.internal/v1
    apiKeyEnv: COMPANY_GATEWAY_API_KEY # or `auth: none`
    headers: { X-Team: "${TEAM_ID}" }  # ${VAR} is read from the environment per request
    discover:                          # list models from GET {baseURL}/models
      include: ["gpt-4.1*", "llama-*"]
      exclude: ["*embedding*"]
      refresh: 5m
    models:                            # optional: labels, the default, `effort: true` for reasoning models
      - { id: gpt-4.1-mini, label: GPT-4.1 mini }
    # streamUsage: false               # for upstreams that reject stream_options
```

- Unknown fields are errors, so an `apiKey:` with a literal key is rejected.
- Globs: `*` matches anything (including `/`), `?` one character.
- `reasoning_effort` is only forwarded to models flagged `effort: true` (Claude: `output_config.effort`).
- Claude's thinking is returned as `reasoning_content` + `thinking_blocks` and must be sent back on the assistant message (the platform does this). Other upstreams never see these fields.
- `GATEWAY_API_KEYS=key1,key2` makes the gateway require `Authorization: Bearer <key>`. Unset means open; keep it on the internal network.
- Check a file before deploying: `make gateway-check` or `go run ./ms-llm-gateway/cmd/gateway check --config my-llms.yaml --discover`.
- `GET :8082/healthz` reports each provider's credentials, model count and discovery result.

Try it directly:

```bash
curl localhost:8082/v1/models
curl localhost:8082/v1/chat/completions -d '{"model":"claude-haiku/claude-haiku-5-5","messages":[{"role":"user","content":"Hi"}]}'
```

### The platform: `ms-ai-platform/config/llm-providers.yaml`

The OpenAI-compatible gateways the platform uses. Each one becomes an LLM in the catalog (`core.llm.<key>`). By default only `ms-llm-gateway` is registered:

```yaml
providers:
  gateway:                                        # core.llm.gateway
    name: LLM Gateway
    baseURL: ${LLM_GATEWAY_URL:-http://localhost:8082/v1}
    apiKeyEnv: LLM_GATEWAY_API_KEY                # sent when set (one of GATEWAY_API_KEYS)
    auth: optional

  # company:                                      # another gateway, used directly
  #   name: Company Gateway
  #   baseURL: https://llm.internal/v1
  #   apiKeyEnv: COMPANY_GATEWAY_API_KEY
  #   include: ["gpt-4.1*"]
```

- `LLM_PROVIDERS_PATH=/path/to/llm-providers.yaml` uses another file.
- Builders pick a gateway (the picker is hidden while there is only one), then a model.
- Guardrail models are `"<gateway>:<model>"`, e.g. `gateway:claude-haiku/claude-haiku-5-5`; `defaults.guardrailModel` sets the default, otherwise the first gateway's default model.
- Drafts saved before the gateway existed (`core.llm.claude-haiku`) are upgraded when read.
- `GET :8080/healthz` reports each gateway's status, model count and last check.

## Tests

```bash
make test-go     # all Go modules (lib-agent-assets, ms-ai-platform, ms-llm-gateway, demo-mcp); set DATABASE_URL_TEST to also run the draft concurrency test
make test-web    # lint, vitest, production build
```

Notable tests:
- `ms-ai-platform/internal/agui/handler_test.go` runs the real compiler and assets end to end against a scripted fake gateway (`internal/llmproviders/gatewaytest`) and an in-process MCP server: hand-off, MCP tool call, Claude thinking round-trip, PII redaction on input and output, prompt-injection block, blocked messages never reaching a model in later turns, gateway errors and outages, and an orchestration spread over two gateways. With `-update` it records `testdata/run.sse`.
- `ms-llm-gateway/internal/server/server_test.go` drives the gateway with the official `openai-go` client against a fake Claude and a fake OpenAI-compatible upstream: a streamed tool loop with the thinking round-trip, passthrough, model listing, errors and auth.
- `ms-llm-gateway/internal/server/live_test.go` (`-tags live`) calls the real Anthropic API through the gateway.
- `web-ai-platform/src/playground/agui-fixture.test.ts` replays that recording through `@ag-ui/client` to prove the wire format matches.

## Adding an asset

1. Implement the `assets.Tool`, `assets.MCP` or `assets.Guardrail` interface (from `pkg/assets`) under `lib-agent-assets/assets/<domain>/<kind>/`. Describe the config as a Go struct; its JSON Schema drives the UI form.
2. Register it in `assets/<domain>/registry.go` (and add the domain to `assets/registry.go`).
3. `make assets-validate`, then restart the API (`make up` restarts it for you). It appears in the sidebar and pickers.

## Slice 0 scope

In: `ms-llm-gateway` (Anthropic and OpenAI-compatible upstreams, model discovery) and one or more gateways registered in the platform; a shared agent library (agents reused across orchestrations, each with its own version) and one shared draft per orchestration, both with `If-Match`/409; `llm` agents with hand-off (sub-agent) and call-as-tool edges; AG-UI events `RUN_*`, `STEP_*`, `TEXT_MESSAGE_*`, `TOOL_CALL_*`, `CUSTOM guardrail.decision` / `message.final`.

Not yet: authentication (run it on the internal network only), publish/revisions/releases, workflow agents (sequential/parallel/loop), human-in-the-loop confirmations, persistent sessions (chat history lives in API memory and is lost on restart), traces, OpenAPI-generated clients, drag-and-drop from the sidebar onto nodes.

Known limits: streamed text is redacted chunk by chunk, so PII split across two chunks can show briefly before the final message replaces it; session history keeps the user's original (unredacted) text, while models only ever see the redacted version.
