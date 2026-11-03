export const API_BASE = "/api"

export class ApiError extends Error {
  status: number
  code: string
  body: unknown

  constructor(status: number, code: string, message: string, body: unknown) {
    super(message)
    this.status = status
    this.code = code
    this.body = body
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json")
  const res = await fetch(API_BASE + path, { ...init, headers })
  if (res.status === 204) return undefined as T
  const text = await res.text()
  const body = text ? JSON.parse(text) : undefined
  if (!res.ok) {
    const err = body?.error ?? {}
    throw new ApiError(res.status, err.code ?? "http_error", err.message ?? res.statusText, body)
  }
  return body as T
}

export function newId(prefix: string): string {
  const alphabet = "abcdefghijkmnpqrstuvwxyz23456789"
  const bytes = crypto.getRandomValues(new Uint8Array(12))
  return `${prefix}_${Array.from(bytes, (b) => alphabet[b % alphabet.length]).join("")}`
}
