// UI preferences only (per device). Storage may be unavailable in a partitioned
// iframe or a private window, so every access is guarded.

const PREFIX = 'dreamer:'

export function readPref(key: string): string | null {
  try {
    return window.localStorage.getItem(PREFIX + key)
  } catch {
    return null
  }
}

export function writePref(key: string, value: string): void {
  try {
    window.localStorage.setItem(PREFIX + key, value)
  } catch {
    // Not persisting a UI preference is harmless.
  }
}
