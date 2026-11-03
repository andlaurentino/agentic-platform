import { HttpAgent, type AgentSubscriber } from "@ag-ui/client"
import { useCallback, useRef, useState } from "react"
import { API_BASE, newId } from "@/api/client"
import type { GuardrailDecision } from "@/api/types"
import { useEditor } from "@/editor/store"

export type ChatItem =
  | { kind: "user"; id: string; text: string }
  | { kind: "assistant"; id: string; text: string; agentId: string | null; streaming: boolean }
  | { kind: "step"; id: string; agentId: string; from: string | null }
  | { kind: "tool"; id: string; name: string; args: string; result?: string; agentId: string | null }
  | { kind: "guardrail"; id: string; decision: GuardrailDecision }
  | { kind: "error"; id: string; message: string }

/** Runs the orchestration draft over AG-UI and keeps a renderable transcript. */
export function usePlayground(orchestrationId: string) {
  const [items, setItems] = useState<ChatItem[]>([])
  const [running, setRunning] = useState(false)
  const agentRef = useRef<HttpAgent | null>(null)
  const threadRef = useRef(newId("thr"))

  const getAgent = useCallback(() => {
    if (!agentRef.current) {
      agentRef.current = new HttpAgent({
        url: `${API_BASE}/v1/orchestrations/${orchestrationId}/agui?target=draft`,
        threadId: threadRef.current,
      })
    }
    return agentRef.current
  }, [orchestrationId])

  const reset = useCallback(() => {
    agentRef.current?.abortRun()
    agentRef.current = null
    threadRef.current = newId("thr")
    setItems([])
    setRunning(false)
    useEditor.getState().setActiveAgent(null)
  }, [])

  const stop = useCallback(() => agentRef.current?.abortRun(), [])

  const send = useCallback(
    async (text: string) => {
      const agent = getAgent()
      const update = (fn: (items: ChatItem[]) => ChatItem[]) => setItems(fn)
      const patch = <K extends ChatItem["kind"]>(kind: K, id: string, fn: (it: Extract<ChatItem, { kind: K }>) => Partial<Extract<ChatItem, { kind: K }>>) =>
        update((xs) => xs.map((x) => (x.kind === kind && x.id === id ? ({ ...x, ...fn(x as Extract<ChatItem, { kind: K }>) } as ChatItem) : x)))

      let step: string | null = null
      const userMsg = { id: newId("msg"), role: "user" as const, content: text }
      agent.addMessage(userMsg)
      update((xs) => [...xs, { kind: "user", id: userMsg.id, text }])
      setRunning(true)

      const subscriber: AgentSubscriber = {
        onStepStartedEvent: ({ event }) => {
          // Capture now: the updater runs later, after `step` has changed.
          const from = step
          update((xs) => [...xs, { kind: "step", id: newId("step"), agentId: event.stepName, from }])
          step = event.stepName
          useEditor.getState().setActiveAgent(event.stepName)
        },
        onTextMessageStartEvent: ({ event }) => {
          const agentId = step
          update((xs) => [...xs, { kind: "assistant", id: event.messageId, text: "", agentId, streaming: true }])
        },
        onTextMessageContentEvent: ({ event }) => {
          patch("assistant", event.messageId, (m) => ({ text: m.text + event.delta }))
        },
        onTextMessageEndEvent: ({ event }) => {
          patch("assistant", event.messageId, () => ({ streaming: false }))
        },
        onToolCallStartEvent: ({ event }) => {
          const agentId = step
          update((xs) => [...xs, { kind: "tool", id: event.toolCallId, name: event.toolCallName, args: "", agentId }])
        },
        onToolCallArgsEvent: ({ event }) => {
          patch("tool", event.toolCallId, (t) => ({ args: t.args + event.delta }))
        },
        onToolCallResultEvent: ({ event }) => {
          const content = typeof event.content === "string" ? event.content : JSON.stringify(event.content)
          patch("tool", event.toolCallId, () => ({ result: content }))
        },
        onCustomEvent: ({ event }) => {
          if (event.name === "guardrail.decision") {
            update((xs) => [...xs, { kind: "guardrail", id: newId("g"), decision: event.value as GuardrailDecision }])
          } else if (event.name === "message.final") {
            const v = event.value as { messageId: string; text: string }
            patch("assistant", v.messageId, () => ({ text: v.text }))
          }
        },
        onRunErrorEvent: ({ event }) => {
          update((xs) => [...xs, { kind: "error", id: newId("err"), message: event.message }])
        },
        onRunFailed: ({ error }) => {
          if (error.name !== "AbortError") update((xs) => [...xs, { kind: "error", id: newId("err"), message: error.message }])
        },
      }

      try {
        await agent.runAgent({ runId: newId("run") }, subscriber)
      } catch (err) {
        const e = err as Error
        if (e.name !== "AbortError") update((xs) => [...xs, { kind: "error", id: newId("err"), message: e.message }])
      } finally {
        setRunning(false)
        update((xs) => xs.map((x) => (x.kind === "assistant" && x.streaming ? { ...x, streaming: false } : x)))
        useEditor.getState().setActiveAgent(null)
      }
    },
    [getAgent],
  )

  return { items, running, send, stop, reset }
}
