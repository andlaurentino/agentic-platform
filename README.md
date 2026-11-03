# Agentic Platform

Internal platform for building conversational AI agents. Engineers register **assets** (LLMs, tools, MCP servers, guardrails) in code; builders combine them into agents and orchestrations in a visual editor and test them in a playground. See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design.

This repo currently contains **slice 0**: a thin, working path through every service.

| Folder | What it is |
|---|---|
| `agent-assets/` | Go module: asset SDK (`sdk/`), registry, `assets` CLI, and the core assets |
| `ms-ai-platform/` | Go API: catalog, skills, orchestrations + draft, spec → Google ADK compiler, AG-UI runtime |
| `web-ai-platform/` | React + Tailwind + shadcn app: sidebar, catalog, skills, canvas editor, playground |
| `demo-mcp/` | Example MCP server (fake weather, orders, FAQ) |

## Run it locally

Requirements: Go 1.27+, Node 24+, Docker (for Postgres), an Anthropic API key with credits.

```bash
cp .env.example .env        # set ANTHROPIC_API_KEY
make db                     # Postgres on :5433
make dev-mcp                # demo-mcp on :8081
make dev-api                # ms-ai-platform on :8080 (runs migrations, syncs the catalog)
make dev-web                # http://localhost:5173
```

Then: **Orchestrations → New orchestration**, add agents on the canvas, drag from an agent's bottom handle to another agent to delegate, pick tools/MCP tools/guardrails/skills in the **Agent** tab, set guardrails for every agent in **Settings**, and chat in **Test**.

Try the AG-UI endpoint without the UI:

```bash
curl -N -X POST "localhost:8080/v1/orchestrations/<id>/agui?target=draft" \
  -d '{"threadId":"t1","runId":"r1","messages":[{"id":"m1","role":"user","content":"Weather in Lisbon?"}]}'
```

`ANTHROPIC_BASE_URL` points the Claude asset at another endpoint (for example a local fake) if you need to run without credits.

## Tests

```bash
make test-go     # all Go modules; set DATABASE_URL_TEST to also run the draft concurrency test
make test-web    # lint, vitest, production build
```

Notable tests:
- `ms-ai-platform/internal/agui/handler_test.go` runs the real compiler and assets end to end against a scripted fake Claude (`agent-assets/assets/core/llms/claudetest`) and an in-process MCP server: hand-off, MCP tool call, PII redaction on input and output, prompt-injection block, and that blocked messages never reach a model in later turns. With `-update` it records `testdata/run.sse`.
- `web-ai-platform/src/playground/agui-fixture.test.ts` replays that recording through `@ag-ui/client` to prove the wire format matches.
- `agent-assets/assets/core/llms/claude_live_test.go` (`-tags live`) calls the real Anthropic API.

## Adding an asset

1. Implement the `sdk.LLM`, `sdk.Tool`, `sdk.MCP` or `sdk.Guardrail` interface under `agent-assets/assets/<domain>/<kind>/`. Describe the config as a Go struct; its JSON Schema drives the UI form.
2. Register it in `assets/<domain>/registry.go` (and add the domain to `assets/registry.go`).
3. `make assets-validate assets-export`, then restart the API. It appears in the sidebar and pickers.

## Slice 0 scope

In: Claude Haiku (`claude-haiku-5-5`, `claude-haiku-4-5`) via `ANTHROPIC_API_KEY`; one shared draft per orchestration with `If-Match`/409; `llm` agents with hand-off (sub-agent) and call-as-tool edges; AG-UI events `RUN_*`, `STEP_*`, `TEXT_MESSAGE_*`, `TOOL_CALL_*`, `CUSTOM guardrail.decision` / `message.final`.

Not yet: authentication (run it on the internal network only), publish/revisions/releases, workflow agents (sequential/parallel/loop), human-in-the-loop confirmations, persistent sessions (chat history lives in API memory and is lost on restart), traces, OpenAPI-generated clients, drag-and-drop from the sidebar onto nodes.

Known limits: streamed text is redacted chunk by chunk, so PII split across two chunks can show briefly before the final message replaces it; session history keeps the user's original (unredacted) text, while models only ever see the redacted version.
