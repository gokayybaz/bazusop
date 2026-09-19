import { useEffect, useState } from "react"
import { ShieldCheck } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { formatLastSeen } from "../lib/format"
import type { AuditEvent } from "../types"

export function AuditPage() {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error">("loading")

  useEffect(() => {
    const controller = new AbortController()
    fetch("/api/v1/audit/events?limit=200", { signal: controller.signal })
      .then((response) => response.ok ? response.json() as Promise<{ events: AuditEvent[] }> : Promise.reject())
      .then((payload) => {
        setEvents(payload.events ?? [])
        setState("ready")
      })
      .catch((error: unknown) => {
        if ((error as { name?: string }).name !== "AbortError") setState("error")
      })
    return () => controller.abort()
  }, [])

  if (state === "loading") return <Card className="empty-feature page-card"><span><ShieldCheck size={24} /></span><h2>Denetim izi yükleniyor</h2><p>İş ve alarm olayları tek zaman çizelgesinde birleştiriliyor.</p></Card>
  if (state === "error") return <EmptyFeature icon={ShieldCheck} title="Denetim iziyle ulaşılamıyor" text="Hub bağlantısını kontrol edin." />
  if (events.length === 0) return <EmptyFeature icon={ShieldCheck} title="Henüz denetim kaydı yok" text="Onaylanan işler ve alarm olayları burada zaman sırasıyla birlikte görünecek." />

  return (
    <Card aria-label="Denetim zaman çizelgesi" className="table-card page-card">
      <div className="card-header">
        <div><h2>Birleşik denetim zaman çizelgesi</h2><p>İş ve alarm olayları · en yeni {events.length} kayıt</p></div>
      </div>
      <div className="table-scroll">
        <table aria-label="Denetim olayları">
          <thead><tr><th>Zaman</th><th>Kaynak</th><th>Olay</th><th>Aktör</th><th>Agent</th><th>Mesaj</th></tr></thead>
          <tbody>
            {events.map((event) => (
              <tr key={`${event.source}-${event.reference_id}-${event.type}-${event.occurred_at}`}>
                <td className="mono muted">{formatLastSeen(event.occurred_at)}</td>
                <td><Badge className={`audit-source ${event.source}`}>{auditSourceLabel(event.source)}</Badge></td>
                <td>{auditEventLabel(event)}</td>
                <td>{event.actor}</td>
                <td className="mono muted">{event.agent_id || "—"}</td>
                <td>{event.message}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  )
}

function auditSourceLabel(source: AuditEvent["source"]) {
  return { job: "İş", alert: "Alarm" }[source]
}

function auditEventLabel(event: AuditEvent) {
  const jobLabels: Record<string, string> = { approved: "Onaylandı", claimed: "Agent teslim aldı", output: "Çıktı", succeeded: "Tamamlandı", failed: "Başarısız" }
  const alertLabels: Record<string, string> = { opened: "Açıldı", acknowledged: "Onaylandı", resolved: "Çözüldü" }
  const labels = event.source === "job" ? jobLabels : alertLabels
  return labels[event.type] ?? event.type
}
