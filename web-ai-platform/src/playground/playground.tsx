import { ArrowRight, ChevronRight, CircleAlert, RotateCcw, Send, ShieldAlert, ShieldCheck, Square, Wrench } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Textarea } from "@/components/ui/textarea"
import { agentName, useEditor } from "@/editor/store"
import { cn } from "@/lib/utils"
import { usePlayground, type ChatItem } from "./use-playground"

function pretty(json: string | undefined) {
  if (!json) return ""
  try {
    return JSON.stringify(JSON.parse(json), null, 2)
  } catch {
    return json
  }
}

function Item({ item }: { item: ChatItem }) {
  const spec = useEditor((s) => s.spec)
  switch (item.kind) {
    case "user":
      return <div className="ml-8 self-end rounded-lg bg-primary px-3 py-2 text-sm whitespace-pre-wrap text-primary-foreground [overflow-wrap:anywhere]">{item.text}</div>
    case "assistant":
      return (
        <div className="mr-8 grid min-w-0 gap-1">
          {item.agentId && <span className="text-[11px] text-muted-foreground">{agentName(spec, item.agentId)}</span>}
          <div className="rounded-lg bg-muted px-3 py-2 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">
            {item.text}
            {item.streaming && <span className="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse bg-foreground/60 align-middle" />}
          </div>
        </div>
      )
    case "step":
      return (
        <div className="flex items-center justify-center gap-1.5 text-[11px] text-muted-foreground">
          {item.from && (
            <>
              <span>{agentName(spec, item.from)}</span>
              <ArrowRight className="size-3" />
            </>
          )}
          <span className="font-medium text-foreground">{agentName(spec, item.agentId)}</span>
        </div>
      )
    case "tool":
      return (
        <Collapsible className="mr-8 rounded-md border text-xs">
          <CollapsibleTrigger className="group flex w-full items-center gap-2 px-2.5 py-1.5 text-left">
            <Wrench className="size-3.5 text-muted-foreground" />
            <code className="font-mono font-medium">{item.name}</code>
            <span className="text-muted-foreground">{item.result === undefined ? "running…" : "done"}</span>
            <ChevronRight className="ml-auto size-3.5 transition-transform group-data-[state=open]:rotate-90" />
          </CollapsibleTrigger>
          <CollapsibleContent className="grid gap-2 border-t px-2.5 py-2">
            <div>
              <p className="mb-1 text-muted-foreground">Arguments</p>
              <pre className="overflow-x-auto rounded bg-muted p-2 font-mono">{pretty(item.args)}</pre>
            </div>
            {item.result !== undefined && (
              <div>
                <p className="mb-1 text-muted-foreground">Result</p>
                <pre className="max-h-48 overflow-auto rounded bg-muted p-2 font-mono">{pretty(item.result)}</pre>
              </div>
            )}
          </CollapsibleContent>
        </Collapsible>
      )
    case "guardrail": {
      const d = item.decision
      const blocked = d.action === "block" || d.action === "escalate"
      return (
        <div
          className={cn(
            "flex items-start gap-2 rounded-md border px-2.5 py-1.5 text-xs",
            blocked ? "border-destructive/40 bg-destructive/5" : "border-amber-500/40 bg-amber-500/5",
          )}
        >
          {blocked ? <ShieldAlert className="mt-0.5 size-3.5 text-destructive" /> : <ShieldCheck className="mt-0.5 size-3.5 text-amber-600" />}
          <div className="grid gap-0.5">
            <span className="font-medium">
              {d.name} {blocked ? "blocked" : d.action === "redact" ? "redacted" : d.action} the {d.stage === "input" ? "message" : "answer"}
              <Badge variant="outline" className="ml-1.5 px-1 py-0 text-[10px]">
                {d.level}
              </Badge>
            </span>
            {d.reason && <span className="text-muted-foreground">{d.reason}</span>}
          </div>
        </div>
      )
    }
    case "error":
      return (
        <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 px-2.5 py-1.5 text-xs text-destructive">
          <CircleAlert className="mt-0.5 size-3.5 shrink-0" />
          <span className="break-words">{item.message}</span>
        </div>
      )
  }
}

export function Playground({ orchestrationId }: { orchestrationId: string }) {
  const { items, running, send, stop, reset } = usePlayground(orchestrationId)
  const [text, setText] = useState("")
  const bottomRef = useRef<HTMLDivElement>(null)
  const hasErrors = useEditor((s) => s.problems.some((p) => p.severity === "error"))
  const unsaved = useEditor((s) => s.editSeq > s.savedSeq)

  useEffect(() => {
    // Braces matter: scrollIntoView returns a Promise in newer browsers, which React would treat as a cleanup.
    void bottomRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [items])

  const submit = () => {
    const t = text.trim()
    if (!t || running) return
    setText("")
    void send(t)
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b px-4 py-2">
        <p className="flex-1 text-xs text-muted-foreground">
          Chats with the saved draft{unsaved ? " (saving your latest edits…)" : ""}.
        </p>
        <Button variant="ghost" size="sm" onClick={reset} disabled={items.length === 0 && !running}>
          <RotateCcw /> New chat
        </Button>
      </div>
      <div className="flex flex-1 flex-col gap-3 overflow-y-auto p-4">
        {items.length === 0 && (
          <p className="m-auto max-w-xs text-center text-sm text-muted-foreground">
            {hasErrors ? "Fix the problems in Settings before testing." : "Send a message to test this orchestration."}
          </p>
        )}
        {items.map((it) => (
          <Item key={`${it.kind}-${it.id}`} item={it} />
        ))}
        <div ref={bottomRef} />
      </div>
      <form
        className="flex items-end gap-2 border-t p-3"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <Textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault()
              submit()
            }
          }}
          placeholder="Ask something…"
          className="max-h-32 min-h-10 resize-none text-sm"
          rows={1}
        />
        {running ? (
          <Button type="button" size="icon" variant="outline" onClick={stop} aria-label="Stop">
            <Square />
          </Button>
        ) : (
          <Button type="submit" size="icon" disabled={!text.trim()} aria-label="Send">
            <Send />
          </Button>
        )}
      </form>
    </div>
  )
}
