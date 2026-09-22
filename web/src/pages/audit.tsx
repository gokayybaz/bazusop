import { useEffect, useState } from "react"
import { ShieldCheck } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { formatLastSeen } from "../lib/format"
import type { AuditTrailEvent } from "../types"

const actorTypeOptions = ["", "human", "agent", "service_account", "legacy_token", "anonymous"] as const
const outcomeOptions = ["", "success", "failure"] as const

export function AuditTrailPage() {
  const [events, setEvents] = useState<AuditTrailEvent[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error" | "forbidden">("loading")
  const [actorType, setActorType] = useState<(typeof actorTypeOptions)[number]>("")
  const [resourceType, setResourceType] = useState("")
  const [outcome, setOutcome] = useState<(typeof outcomeOptions)[number]>("")

  useEffect(() => {
    const controller = new AbortController()
    const params = new URLSearchParams({ limit: "200" })
    if (actorType) params.set("actor_type", actorType)
    if (resourceType) params.set("resource_type", resourceType)
    if (outcome) params.set("outcome", outcome)
    setState("loading")
    fetch(`/api/v1/audit/events?${params.toString()}`, { signal: controller.signal })
      .then((response) => {
        if (response.status === 401 || response.status === 403) return Promise.reject(new Error("forbidden"))
        if (!response.ok) return Promise.reject(new Error("unavailable"))
        return response.json() as Promise<{ events: AuditTrailEvent[] }>
      })
      .then((payload) => {
        setEvents(payload.events ?? [])
        setState("ready")
      })
      .catch((error: unknown) => {
        if ((error as { name?: string }).name === "AbortError") return
        setState((error as Error).message === "forbidden" ? "forbidden" : "error")
      })
    return () => controller.abort()
  }, [actorType, resourceType, outcome])

  function downloadCSV() {
    const header = ["occurred_at", "actor_type", "actor_id", "action", "resource_type", "resource_id", "outcome", "error_code"]
    const rows = events.map((event) => [event.occurred_at, event.actor_type, event.actor_id, event.action, event.resource_type, event.resource_id, event.outcome, event.error_code])
    const csv = [header, ...rows].map((row) => row.map((cell) => `"${String(cell).replaceAll('"', '""')}"`).join(",")).join("\n")
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" })
    const url = URL.createObjectURL(blob)
    const link = document.createElement("a")
    link.href = url
    link.download = `denetim-kaydi-${new Date().toISOString().slice(0, 10)}.csv`
    link.click()
    URL.revokeObjectURL(url)
  }

  if (state === "forbidden") return <EmptyFeature icon={ShieldCheck} title="Denetim kaydına erişim yetkiniz yok" text="Bu ekranı görüntülemek için site-admin veya operator rolü ya da platform yöneticisi oturumu gerekir." />
  if (state === "error") return <EmptyFeature icon={ShieldCheck} title="Denetim kaydına ulaşılamıyor" text="Hub bağlantısını kontrol edin." />

  return (
    <Card aria-label="Denetim kaydı arama" className="table-card page-card">
      <div className="card-header">
        <div><h2>Denetim kaydı</h2><p>Her API isteğinin başarı/başarısızlık sonucu · en yeni {events.length} kayıt</p></div>
        <button className="text-button" disabled={events.length === 0} onClick={downloadCSV} type="button">CSV indir</button>
      </div>
      <div>
        <label>Aktör türü
          <select aria-label="Aktör türü filtresi" onChange={(event) => setActorType(event.target.value as typeof actorType)} value={actorType}>
            <option value="">Tümü</option>
            <option value="human">İnsan</option>
            <option value="agent">Agent</option>
            <option value="service_account">Servis hesabı</option>
            <option value="legacy_token">Eski token</option>
            <option value="anonymous">Anonim</option>
          </select>
        </label>
        <label>Kaynak türü
          <input aria-label="Kaynak türü filtresi" onChange={(event) => setResourceType(event.target.value)} placeholder="ör. service_accounts" type="text" value={resourceType} />
        </label>
        <label>Sonuç
          <select aria-label="Sonuç filtresi" onChange={(event) => setOutcome(event.target.value as typeof outcome)} value={outcome}>
            <option value="">Tümü</option>
            <option value="success">Başarılı</option>
            <option value="failure">Başarısız</option>
          </select>
        </label>
      </div>
      {state === "loading" ? <p className="muted">Yükleniyor…</p> : events.length === 0 ? <p className="muted">Bu filtrelerle eşleşen kayıt yok.</p> : (
        <div className="table-scroll">
          <table aria-label="Denetim kayıtları">
            <thead><tr><th>Zaman</th><th>Aktör</th><th>Eylem</th><th>Kaynak</th><th>Sonuç</th></tr></thead>
            <tbody>
              {events.map((event) => (
                <tr key={event.event_id}>
                  <td className="mono muted">{formatLastSeen(event.occurred_at)}</td>
                  <td>{actorTypeLabel(event.actor_type)}</td>
                  <td className="mono">{event.action} {event.resource_type}</td>
                  <td className="mono muted">{event.resource_id || "—"}</td>
                  <td><Badge className={`audit-outcome ${event.outcome}`}>{event.outcome === "success" ? "Başarılı" : `Başarısız (${event.error_code})`}</Badge></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}

function actorTypeLabel(actorType: AuditTrailEvent["actor_type"]) {
  return { human: "İnsan", agent: "Agent", service_account: "Servis hesabı", legacy_token: "Eski token", anonymous: "Anonim" }[actorType] ?? actorType
}
