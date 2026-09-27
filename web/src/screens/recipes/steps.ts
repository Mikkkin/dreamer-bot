// Splits a recipe's plain text into lines for display: "1. …" / "2) …" lines
// become numbered steps, blank lines become gaps, everything else stays text.
// The result is rendered as React text nodes; nothing is ever parsed as HTML.

export type RecipeLine = { kind: 'step'; n: string; text: string } | { kind: 'text'; text: string } | { kind: 'gap' }

const STEP_RE = /^\s*(\d{1,3})[.)]\s+(.+)$/

export function recipeLines(body: string): RecipeLine[] {
  const out: RecipeLine[] = []
  for (const raw of body.replace(/\r\n?/g, '\n').split('\n')) {
    if (raw.trim() === '') {
      if (out.length > 0 && out.at(-1)?.kind !== 'gap') out.push({ kind: 'gap' })
      continue
    }
    const m = STEP_RE.exec(raw)
    if (m?.[1] && m[2]) out.push({ kind: 'step', n: m[1], text: m[2].trim() })
    else out.push({ kind: 'text', text: raw.trimEnd() })
  }
  if (out.at(-1)?.kind === 'gap') out.pop()
  return out
}
