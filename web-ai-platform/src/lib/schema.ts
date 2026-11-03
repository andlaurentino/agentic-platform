import type { JSONSchema } from "@/api/types"

export function hasFields(schema?: JSONSchema): boolean {
  return Object.keys(schema?.properties ?? {}).length > 0
}
