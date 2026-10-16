/** A copy of the list with the item of that id replaced by `fn(item)`; other items are shared, not copied. */
export function patchItem<T extends { id: string }>(
  items: readonly T[] | null | undefined,
  id: string,
  fn: (item: T) => T,
): T[] {
  return (items ?? []).map((item) => (item.id === id ? fn(item) : item))
}

/** Drops repeated ids, keeping the first of each in its original order. */
export function dedupeById<T extends { id: string }>(items: readonly T[] | null | undefined): T[] {
  const seen = new Set<string>()
  const out: T[] = []
  for (const item of items ?? []) {
    if (seen.has(item.id)) continue
    seen.add(item.id)
    out.push(item)
  }
  return out
}
