import { useEffect, useState } from "react"
import { Search } from "lucide-react"

import { Card } from "../components/ui/card"
import { ServiceFilterButton } from "../components/filter-button"
import { formatLogTime } from "../lib/format"
import type { InventoryInstance, LogEntry } from "../types"

type LogFilter = "all" | LogEntry["severity"]

export function LogPanel({ entries, instance, state }: {
  entries: LogEntry[]
  instance: InventoryInstance
  state: "idle" | "loading" | "ready" | "error"
}) {
  const [filter, setFilter] = useState<LogFilter>("all")
  const [query, setQuery] = useState("")
  const [live, setLive] = useState(false)
  const [streamEntries, setStreamEntries] = useState<LogEntry[]>([])

  useEffect(() => {
    if (!live) return
    const stream = new EventSource(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/logs/stream`)
    const receive = (event: MessageEvent<string>) => {
      try {
        const entry = JSON.parse(event.data) as LogEntry
        setStreamEntries((current) => [...current.slice(-199), entry])
      } catch {
        // Ignore malformed frames and keep the live stream open.
      }
    }
    stream.addEventListener("log", receive as EventListener)
    return () => stream.close()
  }, [instance.agent_id, live])

  const normalizedQuery = query.trim().toLocaleLowerCase("tr-TR")
  const visibleEntries = [...entries, ...streamEntries].filter((entry) => {
    const matchesSeverity = filter === "all" || entry.severity === filter
    const searchable = `${entry.source} ${entry.message}`.toLocaleLowerCase("tr-TR")
    return matchesSeverity && (normalizedQuery === "" || searchable.includes(normalizedQuery))
  })

  return (
    <Card aria-label={`${instance.hostname} logları`} className="logs-card">
      <div className="card-header logs-header">
        <div><h2>Log akışı</h2><p>{instance.hostname} · son 1 saat · en fazla 200 kayıt</p></div>
        <div className="log-actions">
          <label className="service-search log-search">
            <Search aria-hidden="true" size={15} />
            <input aria-label="Loglarda ara" onChange={(event) => setQuery(event.target.value)} placeholder="Mesaj veya kaynak ara…" type="search" value={query} />
          </label>
          <button aria-pressed={live} className={live ? "live-button active" : "live-button"} onClick={() => setLive((current) => !current)} type="button">
            <span className="status-dot" />{live ? "Canlı akışı durdur" : "Canlı akışı başlat"}
          </button>
        </div>
      </div>
      <div aria-label="Log önem filtresi" className="service-filters log-filters" role="group">
        <ServiceFilterButton active={filter === "all"} label="Tümü" onClick={() => setFilter("all")} />
        <ServiceFilterButton active={filter === "debug"} label="Debug" onClick={() => setFilter("debug")} />
        <ServiceFilterButton active={filter === "info"} label="Bilgi" onClick={() => setFilter("info")} />
        <ServiceFilterButton active={filter === "warn"} label="Uyarı" onClick={() => setFilter("warn")} />
        <ServiceFilterButton active={filter === "error"} label="Hata" onClick={() => setFilter("error")} accessibleLabel="Hata loglarını göster" />
        <ServiceFilterButton active={filter === "critical"} label="Kritik" onClick={() => setFilter("critical")} />
      </div>
      {state === "loading" ? <div className="service-state">Loglar yükleniyor…</div> : null}
      {state === "error" ? <div className="service-state">Log geçmişine şu anda ulaşılamıyor.</div> : null}
      {state === "ready" && visibleEntries.length === 0 ? <div className="service-state">Bu filtreyle eşleşen log kaydı yok.</div> : null}
      {visibleEntries.length > 0 ? (
        <div aria-live={live ? "polite" : "off"} className="log-console">
          {visibleEntries.map((entry) => (
            <div className={`log-line ${entry.severity}`} key={`${entry.id}-${entry.occurred_at}`}>
              <time dateTime={entry.occurred_at}>{formatLogTime(entry.occurred_at)}</time>
              <span className="log-severity">{logSeverityLabel(entry.severity)}</span>
              <span className="log-source">{entry.source}</span>
              <span className="log-message">{entry.message}</span>
            </div>
          ))}
        </div>
      ) : null}
    </Card>
  )
}

function logSeverityLabel(severity: LogEntry["severity"]) {
  return { debug: "DEBUG", info: "BİLGİ", warn: "UYARI", error: "HATA", critical: "KRİTİK" }[severity]
}
