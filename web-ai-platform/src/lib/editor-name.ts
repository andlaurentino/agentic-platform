// No authentication yet, so people type their name and it is attached to draft saves.
const KEY = "ai-platform.editor-name"

export function getEditorName(): string {
  try {
    return localStorage.getItem(KEY) ?? ""
  } catch {
    return ""
  }
}

export function setEditorName(name: string) {
  try {
    localStorage.setItem(KEY, name)
  } catch {
    // storage unavailable; the name just isn't remembered
  }
}
