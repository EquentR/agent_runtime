export function formatCompactTimestamp(value?: string) {
  if (!value) {
    return '--'
  }
  return value.replace('T', ' ').slice(0, 16)
}
