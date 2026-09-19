export function formatMemory(bytes: number) {
  return `${Math.round(bytes / 1024 / 1024 / 1024)} GiB`
}

export function formatLastSeen(value: string) {
  const timestamp = new Date(value)
  if (Number.isNaN(timestamp.getTime())) return "Bilinmiyor"
  return timestamp.toLocaleString("tr-TR", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
}

export function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`
}

export function formatLogTime(value: string) {
  const timestamp = new Date(value)
  if (Number.isNaN(timestamp.getTime())) return "--:--:--"
  return timestamp.toLocaleTimeString("tr-TR", { hour: "2-digit", minute: "2-digit", second: "2-digit" })
}

export function formatBuildDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("tr-TR", { dateStyle: "medium", timeStyle: "short" })
}
