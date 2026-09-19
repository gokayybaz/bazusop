import { ChevronRight, CircleAlert } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { formatLastSeen } from "../lib/format"
import type { AlertIncident, InventoryInstance } from "../types"

export function OverviewPage({ inventoryState, instances, alertState, activeIncidents, onOpenAlarmCenter }: {
  inventoryState: "loading" | "ready" | "error"
  instances: InventoryInstance[]
  alertState: "loading" | "ready" | "error"
  activeIncidents: AlertIncident[]
  onOpenAlarmCenter: () => void
}) {
  const connectedInstances = instances.filter((instance) => instance.status === "connected").length
  const criticalIncidents = activeIncidents.filter((incident) => incident.severity === "critical").length

  return (
    <>
      <section className="stat-grid" aria-label="Filo özeti">
        <Metric
          label="Sunucular"
          value={inventoryState === "loading" ? "—" : String(instances.length)}
          detail={inventoryState === "error" ? "Envantere ulaşılamıyor" : `${connectedInstances} bağlı`}
          trend={inventoryState === "ready" ? "Canlı envanter" : "Hub bekleniyor"}
        />
        <Metric label="Ortalama CPU" value="42.8%" detail="24 saatlik filo ortalaması" trend="düne göre −%3,2" />
        <Metric label="Açık alarmlar" value={alertState === "loading" ? "—" : String(activeIncidents.length)} detail={`${criticalIncidents} kritik alarm`} trend={`${activeIncidents.filter((incident) => incident.status === "acknowledged").length} alarm onaylandı`} alert />
      </section>

      <div className="dashboard-grid">
        <Card className="chart-card">
          <div className="card-header">
            <div><h2>Filo kaynak kullanımı</h2><p>CPU ve bellek · son 24 saat</p></div>
            <div className="chart-legend"><span className="cpu">CPU</span><span className="memory">Bellek</span></div>
          </div>
          <ResourceChart />
        </Card>

        <Card aria-label="Operasyon alarmları" className="alerts-card">
          <div className="card-header"><div><h2>Operasyon alarmları</h2><p>İlgilenilmesi gereken sinyaller</p></div><Badge className="critical-count">{activeIncidents.length} açık</Badge></div>
          <div className="alert-list">
            {activeIncidents.slice(0, 3).map((incident) => <Alert host={incident.agent_id} key={incident.id} level={incident.severity === "critical" ? "Kritik" : "Uyarı"} meta={`${incident.message} · ${formatLastSeen(incident.opened_at)}`} title={incident.rule_name} />)}
            {alertState === "loading" ? <div className="alert-empty">Alarmlar yükleniyor…</div> : null}
            {alertState === "error" ? <div className="alert-empty">Alarm verisine ulaşılamıyor.</div> : null}
            {alertState === "ready" && activeIncidents.length === 0 ? <div className="alert-empty">Açık alarm yok.</div> : null}
          </div>
          <button aria-label="Alarm merkezini aç" className="panel-link" onClick={onOpenAlarmCenter} type="button">Alarm merkezini aç <ChevronRight size={15} /></button>
        </Card>
      </div>
    </>
  )
}

function Metric({ label, value, detail, trend, alert = false }: { label: string; value: string; detail: string; trend: string; alert?: boolean }) {
  return (
    <Card className={alert ? "metric-card metric-alert" : "metric-card"}>
      <span className="metric-label">{label}</span>
      <div className="metric-value"><strong>{value}</strong>{alert ? <span className="alert-pip">1 kritik</span> : null}</div>
      <div className="metric-detail"><span>{detail}</span><span className={alert ? "trend alert" : "trend"}>{trend}</span></div>
    </Card>
  )
}

function ResourceChart() {
  return (
    <div className="chart-wrap">
      <div className="chart-scale"><span>100%</span><span>75%</span><span>50%</span><span>25%</span><span>0%</span></div>
      <svg aria-label="Son 24 saatte filo kaynak kullanımı" className="resource-chart" role="img" viewBox="0 0 800 250">
        <defs>
          <linearGradient id="cpu-area" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stopColor="var(--accent)" stopOpacity=".28"/><stop offset="1" stopColor="var(--accent)" stopOpacity="0"/></linearGradient>
          <linearGradient id="memory-area" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stopColor="var(--success)" stopOpacity=".14"/><stop offset="1" stopColor="var(--success)" stopOpacity="0"/></linearGradient>
        </defs>
        {[20, 72, 124, 176, 228].map((y) => <line className="grid-line" key={y} x1="0" x2="800" y1={y} y2={y} />)}
        <path className="memory-area" d="M0 154 C65 140 94 158 142 133 S230 118 282 139 S360 103 419 116 S492 84 550 101 S628 69 682 92 S742 63 800 77 L800 228 L0 228 Z" />
        <path className="memory-line" d="M0 154 C65 140 94 158 142 133 S230 118 282 139 S360 103 419 116 S492 84 550 101 S628 69 682 92 S742 63 800 77" />
        <path className="cpu-area" d="M0 184 C48 172 78 190 116 163 S188 146 228 158 S288 117 334 139 S411 84 452 112 S514 92 558 119 S620 75 659 97 S724 54 753 82 S783 60 800 68 L800 228 L0 228 Z" />
        <path className="cpu-line" d="M0 184 C48 172 78 190 116 163 S188 146 228 158 S288 117 334 139 S411 84 452 112 S514 92 558 119 S620 75 659 97 S724 54 753 82 S783 60 800 68" />
        <circle className="chart-point" cx="800" cy="68" r="4" />
      </svg>
      <div className="chart-axis"><span>00:00</span><span>04:00</span><span>08:00</span><span>12:00</span><span>16:00</span><span>20:00</span><span>Şimdi</span></div>
    </div>
  )
}

function Alert({ level, title, host, meta }: { level: "Kritik" | "Uyarı"; title: string; host: string; meta: string }) {
  return (
    <div className={level === "Kritik" ? "alert-item critical" : "alert-item warning-item"}>
      <CircleAlert size={18} />
      <div><span className="alert-level">{level}</span><strong>{title}</strong><span className="alert-host">{host}</span><small>{meta}</small></div>
    </div>
  )
}
