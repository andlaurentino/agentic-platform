import { Check, ChevronsUpDown } from "lucide-react"
import { useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { cn } from "@/lib/utils"

export interface Option {
  value: string
  label: string
}

// Short lists use a plain select; long ones (e.g. models discovered from a gateway) get a search box.
const SEARCH_THRESHOLD = 15

export function OptionPicker({
  id,
  options,
  value,
  defaultValue,
  onChange,
  placeholder = "Choose…",
}: {
  id?: string
  options: Option[]
  value: string | undefined
  defaultValue?: string
  onChange: (v: string | undefined) => void
  placeholder?: string
}) {
  const current = value ?? defaultValue ?? ""
  const label = (o: Option) => (o.value === defaultValue ? `${o.label} (default)` : o.label)

  if (options.length <= SEARCH_THRESHOLD) {
    return (
      <Select value={current} onValueChange={onChange}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {label(o)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    )
  }
  return <SearchablePicker id={id} options={options} current={current} label={label} onChange={onChange} placeholder={placeholder} />
}

function SearchablePicker({
  id,
  options,
  current,
  label,
  onChange,
  placeholder,
}: {
  id?: string
  options: Option[]
  current: string
  label: (o: Option) => string
  onChange: (v: string | undefined) => void
  placeholder: string
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? options.filter((o) => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q)) : options
  }, [options, query])
  const selected = options.find((o) => o.value === current)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button id={id} variant="outline" role="combobox" aria-expanded={open} className="w-full justify-between font-normal">
          <span className={cn("truncate", !selected && "text-muted-foreground")}>{selected ? label(selected) : placeholder}</span>
          <ChevronsUpDown className="opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-(--radix-popover-trigger-width) p-0" align="start">
        <div className="border-b p-2">
          <Input autoFocus value={query} onChange={(e) => setQuery(e.target.value)} placeholder={`Search ${options.length} options…`} className="h-8" />
        </div>
        <ul className="max-h-64 overflow-y-auto p-1" role="listbox">
          {filtered.length === 0 && <li className="px-2 py-1.5 text-sm text-muted-foreground">No match</li>}
          {filtered.map((o) => (
            <li key={o.value} role="option" aria-selected={o.value === current}>
              <button
                type="button"
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent"
                onClick={() => {
                  onChange(o.value)
                  setOpen(false)
                  setQuery("")
                }}
              >
                <Check className={cn("size-4", o.value === current ? "opacity-100" : "opacity-0")} />
                <span className="truncate">{label(o)}</span>
              </button>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  )
}
