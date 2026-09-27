// Russian typesetting for display titles: a short function word (в, и, на,
// со, у…) is glued to the next word with a no-break space, so a line never
// ends with a dangling preposition. Only real prepositions, conjunctions and
// particles are glued — a short noun such as «ям» in «Том ям» is not, or a
// chain of glued words would force an ugly break. Plain string in and out.

const NBSP = '\u00a0'

// One- and two-letter Russian prepositions, conjunctions, particles and
// pronouns that read badly at the end of a line.
const FUNCTION_WORDS = new Set([
  'а', 'в', 'и', 'к', 'о', 'с', 'у', 'я',
  'во', 'да', 'до', 'же', 'за', 'из', 'ко', 'мы', 'на', 'не', 'ни', 'но',
  'об', 'он', 'от', 'по', 'со', 'то', 'ты',
])

const LEADING_QUOTES = /^[«"„(]+/

function isFunctionWord(word: string): boolean {
  return FUNCTION_WORDS.has(word.replace(LEADING_QUOTES, '').toLowerCase())
}

export function glueShortWords(text: string): string {
  const words = text.split(' ')
  let out = words[0] ?? ''
  for (let i = 1; i < words.length; i++) {
    const prev = words[i - 1] ?? ''
    out += (isFunctionWord(prev) ? NBSP : ' ') + words[i]
  }
  return out
}
