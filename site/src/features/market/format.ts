const countFormatter = new Intl.NumberFormat('zh-CN')

/** Group a plain count (requests, listings); never used for money. */
export function formatCount(value: number | string): string {
  const number = typeof value === 'string' ? Number(value) : value
  if (!Number.isFinite(number)) return '—'
  return countFormatter.format(number)
}

/** Context window in decimal units: 128000 → "128K", 1000000 → "1M". */
export function formatContext(tokens: number): string {
  if (!Number.isFinite(tokens) || tokens <= 0) return ''
  if (tokens >= 1_000_000) return `${Number((tokens / 1_000_000).toFixed(1))}M`
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}K`
  return String(tokens)
}
