import { useEffect, useState } from "react"
import { Activity as ActivityIcon } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { formatLastSeen } from "../lib/format"
import type { ActivityEvent } from "../types"

export function ActivityPage() {
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error">("loading")

  useEffect(() => {
    const controller = new AbortController()
    fetch("/api/v1/activity/events?limit=200", { signal: controller.signal })
      .then((response) => response.ok ? response.json() as Promise<{ events: ActivityEvent[] }> : Promise.reject())
      .then((payload) => {
        setEvents(payload.events ?? [])
        setState("ready")
      })
      .catch((error: unknown) => {
        if ((error as { name?: string }).name !== "AbortError") setState("error")
      })
    return () => controller.abort()
  }, [])

  if (state === "loading") return <Card className="empty-feature page-card"><span><ActivityIcon size={24} /></span><h2>Aktivite yükleniyor</h2><p>İş, alarm, davet ve servis hesabı olayları tek zaman çizelgesinde birleştiriliyor.</p></Card>
  if (state === "error") return <EmptyFeature icon={ActivityIcon} title="Aktiviteye ulaşılamıyor" text="Hub bağlantısını veya oturum izninizi kontrol edin." />
  if (events.length === 0) return <EmptyFeature icon={ActivityIcon} title="Henüz aktivite yok" text="Onaylanan işler, alarm olayları, davetler ve servis hesabı değişiklikleri burada zaman sırasıyla görünecek." />

  return (
    <Card aria-label="Aktivite zaman çizelgesi" className="table-card page-card">
      <div className="card-header">
        <div><h2>Aktivite zaman çizelgesi</h2><p>İş, alarm, kimlik ve servis hesabı olayları · en yeni {events.length} kayıt</p></div>
      </div>
      <div className="table-scroll">
        <table aria-label="Aktivite olayları">
          <thead><tr><th>Zaman</th><th>Kaynak</th><th>Olay</th><th>Aktör</th><th>Agent</th><th>Mesaj</th></tr></thead>
          <tbody>
            {events.map((event) => (
              <tr key={`${event.source}-${event.reference_id}-${event.type}-${event.occurred_at}`}>
                <td className="mono muted">{formatLastSeen(event.occurred_at)}</td>
                <td><Badge className={`audit-source ${event.source}`}>{activitySourceLabel(event.source)}</Badge></td>
                <td>{activityEventLabel(event)}</td>
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

function activitySourceLabel(source: ActivityEvent["source"]) {
  return { job: "İş", alert: "Alarm", identity: "Kimlik", site_role: "Site rolü", service_account: "Servis hesabı" }[source]
}

function activityEventLabel(event: ActivityEvent) {
  const labels: Record<ActivityEvent["source"], Record<string, string>> = {
    job: { approved: "Onaylandı", claimed: "Agent teslim aldı", output: "Çıktı", succeeded: "Tamamlandı", failed: "Başarısız" },
    alert: { opened: "Açıldı", acknowledged: "Onaylandı", resolved: "Çözüldü" },
    identity: { invite_created: "Davet edildi" },
    site_role: { assigned: "Rol atandı", revoked: "Rol kaldırıldı" },
    service_account: { created: "Oluşturuldu", token_rotated: "Token rotate edildi", token_revoked: "Token iptal edildi", disabled: "Devre dışı bırakıldı" },
  }
  return labels[event.source]?.[event.type] ?? event.type
}
