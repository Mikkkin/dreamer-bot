// Client-side search over already loaded lists: Unicode-aware, case-insensitive,
// and treats «ё» as «е» the way Russian speakers type.

export function foldText(s: string): string {
  return s.normalize('NFKC').toLocaleLowerCase('ru').replaceAll('ё', 'е')
}

/** Every whitespace-separated term of the query must occur in one of the fields. */
export function matchesQuery(query: string, ...fields: string[]): boolean {
  const terms = foldText(query).split(/\s+/u).filter(Boolean)
  if (terms.length === 0) return true
  const haystack = fields.map(foldText).join('\n')
  return terms.every((t) => haystack.includes(t))
}
