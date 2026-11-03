// Renders a form for an asset's config from its JSON Schema, so new assets
// appear in the UI without frontend changes. Supports the subset of JSON
// Schema our assets use: string/number/integer/boolean, enums, and arrays of enums.
import { OptionPicker } from "@/components/option-picker"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import type { Config, JSONSchema } from "@/api/types"

function typeOf(s: JSONSchema): string {
  return Array.isArray(s.type) ? (s.type.find((t) => t !== "null") ?? "string") : (s.type ?? "string")
}

export function SchemaForm({
  schema,
  value,
  onChange,
  idPrefix,
}: {
  schema: JSONSchema
  value: Config | undefined
  onChange: (next: Config) => void
  idPrefix: string
}) {
  const props = Object.entries(schema.properties ?? {})
  if (props.length === 0) return null
  const current = value ?? {}

  const set = (key: string, v: unknown) => {
    const next = { ...current }
    if (v === undefined || v === "") delete next[key]
    else next[key] = v
    onChange(next)
  }

  return (
    <div className="grid gap-3">
      {props.map(([key, prop]) => {
        const id = `${idPrefix}-${key}`
        const label = prop.title ?? key
        const type = typeOf(prop)
        const v = current[key]
        const def = prop.default

        let field
        if (type === "boolean") {
          field = <Switch id={id} checked={(v ?? def ?? false) as boolean} onCheckedChange={(c) => set(key, c)} />
        } else if (prop.oneOf || prop.enum) {
          const options = prop.oneOf
            ? prop.oneOf.map((o) => ({ value: String(o.const), label: o.title ?? String(o.const) }))
            : prop.enum!.map((o) => ({ value: String(o), label: String(o) }))
          field = (
            <OptionPicker
              id={id}
              options={options}
              value={v === undefined ? undefined : String(v)}
              defaultValue={def === undefined ? undefined : String(def)}
              onChange={(nv) => set(key, nv)}
              placeholder={def === undefined && prop.oneOf ? "Platform default" : "Choose…"}
            />
          )
        } else if (type === "array" && prop.items?.enum) {
          const selected = (v ?? def ?? []) as unknown[]
          field = (
            <div className="flex flex-wrap gap-x-4 gap-y-2">
              {prop.items.enum.map((opt) => {
                const checked = selected.includes(opt)
                return (
                  <label key={String(opt)} className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={checked}
                      onCheckedChange={(c) => set(key, c ? [...selected, opt] : selected.filter((x) => x !== opt))}
                    />
                    {String(opt)}
                  </label>
                )
              })}
            </div>
          )
        } else if (type === "integer" || type === "number") {
          field = (
            <Input
              id={id}
              type="number"
              min={prop.minimum}
              max={prop.maximum}
              value={v === undefined ? "" : String(v)}
              placeholder={def !== undefined ? String(def) : undefined}
              onChange={(e) => set(key, e.target.value === "" ? undefined : Number(e.target.value))}
            />
          )
        } else {
          field = (
            <Input
              id={id}
              value={(v as string) ?? ""}
              placeholder={def !== undefined ? String(def) : undefined}
              onChange={(e) => set(key, e.target.value)}
            />
          )
        }

        return (
          <div key={key} className="grid gap-1.5">
            <Label htmlFor={id} className="text-xs">
              {label}
            </Label>
            {field}
            {prop.description && <p className="text-xs text-muted-foreground">{prop.description}</p>}
          </div>
        )
      })}
    </div>
  )
}
