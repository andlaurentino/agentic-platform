// Replays the SSE stream recorded by the Go AG-UI handler test
// (ms-ai-platform/internal/agui/testdata/run.sse) through the real AG-UI
// client, which validates every event and the event ordering.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { HttpAgent, type BaseEvent } from "@ag-ui/client";
import { describe, expect, it } from "vitest";

const fixture = readFileSync(
  fileURLToPath(new URL("../../../ms-ai-platform/internal/agui/testdata/run.sse", import.meta.url)),
  "utf8",
);

describe("AG-UI wire format", () => {
  it("parses the Go server's event stream", async () => {
    const agent = new HttpAgent({
      url: "http://test/agui",
      threadId: "thr_test",
      fetch: async () =>
        new Response(fixture, { status: 200, headers: { "Content-Type": "text/event-stream" } }),
    });
    const seen: string[] = [];
    let failed: Error | undefined;
    let text = "";
    await agent.runAgent(
      { runId: "run_test" },
      {
        onEvent: ({ event }: { event: BaseEvent }) => {
          seen.push(event.type);
        },
        onTextMessageContentEvent: ({ event }) => {
          text += event.delta;
        },
        onRunFailed: ({ error }) => {
          failed = error;
        },
      },
    );
    expect(failed).toBeUndefined();
    expect(seen[0]).toBe("RUN_STARTED");
    expect(seen.at(-1)).toBe("RUN_FINISHED");
    expect(seen).toContain("TOOL_CALL_RESULT");
    expect(seen.filter((t) => t === "CUSTOM")).toHaveLength(2);
    expect(text).toContain("[EMAIL]");
    // The assistant message ends up in the agent's message list.
    expect(agent.messages.some((m) => m.role === "assistant")).toBe(true);
  });
});
