import {
  Bell,
  Boxes,
  ChartNoAxesCombined,
  ChevronRight,
  CircleAlert,
  Cloud,
  Command,
  Gauge,
  ListChecks,
  Search,
  Server,
  Settings,
  ShieldCheck,
  TerminalSquare,
  X,
} from "lucide-react"
import { useEffect, useState } from "react"

import { ThemeToggle } from "./components/theme-toggle"
import { Badge } from "./components/ui/badge"
import { Card } from "./components/ui/card"

const navigation = [
  { icon: Gauge, label: "Genel bakış", active: true },
  { icon: Server, label: "Filo" },
  { icon: Boxes, label: "Servisler" },
  { icon: ChartNoAxesCombined, label: "Metrikler" },
  { icon: TerminalSquare, label: "Loglar" },
  { icon: ListChecks, label: "İşler" },
  { icon: Bell, label: "Alarmlar", count: 3 },
]

type InventoryInstance = {
  agent_id: string
  hostname: string
  os_family: "linux" | "windows"
  os_name: string
  os_version: string
  architecture: string
  kernel_version: string
  cpu_cores: number
  memory_bytes: number
  ip_addresses: string[]
  agent_version: string
  first_seen_at: string
  last_seen_at: string
  status: "connected" | "stale"
}

type TelemetrySample = {
  recorded_at: string
  cpu_percent: number
  memory_percent: number
  disk_percent: number
  network_rx_bytes: number
  network_tx_bytes: number
}

type TelemetryPayload = {
  latest: TelemetrySample | null
  samples: TelemetrySample[]
}

type ManagedService = {
  agent_id: string
  name: string
  display_name: string
  state: "running" | "stopped" | "failed" | "unknown"
  startup_type: "automatic" | "manual" | "disabled" | "unknown"
  observed_at: string
}

export function App() {
  const [instances, setInstances] = useState<InventoryInstance[]>([])
  const [inventoryState, setInventoryState] = useState<"loading" | "ready" | "error">("loading")
  const [selectedInstance, setSelectedInstance] = useState<InventoryInstance | null>(null)
  const [telemetry, setTelemetry] = useState<TelemetryPayload | null>(null)
  const [telemetryState, setTelemetryState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [services, setServices] = useState<ManagedService[]>([])
  const [serviceState, setServiceState] = useState<"idle" | "loading" | "ready" | "error">("idle")

  useEffect(() => {
    const controller = new AbortController()
    fetch("/api/v1/instances", { signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error("inventory unavailable")
        return response.json() as Promise<{ instances: InventoryInstance[] }>
      })
      .then((payload) => {
        setInstances(payload.instances)
        setInventoryState("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setInventoryState("error")
      })
    return () => controller.abort()
  }, [])

  const connectedInstances = instances.filter((instance) => instance.status === "connected").length

  function openTelemetry(instance: InventoryInstance) {
    setSelectedInstance(instance)
    setTelemetry(null)
    setTelemetryState("loading")
    setServices([])
    setServiceState("loading")
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/telemetry?limit=288`)
      .then((response) => {
        if (!response.ok) throw new Error("telemetry unavailable")
        return response.json() as Promise<TelemetryPayload>
      })
      .then((payload) => {
        setTelemetry(payload)
        setTelemetryState("ready")
      })
      .catch(() => setTelemetryState("error"))
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/services`)
      .then((response) => {
        if (!response.ok) throw new Error("service inventory unavailable")
        return response.json() as Promise<{ services: ManagedService[] }>
      })
      .then((payload) => {
        setServices(payload.services)
        setServiceState("ready")
      })
      .catch(() => setServiceState("error"))
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <div className="app-frame">
        <aside className="sidebar">
          <div className="brand">
            <div className="brand-mark" aria-hidden="true">U</div>
            <div><strong>bazUSOP</strong><span>kontrol düzlemi</span></div>
          </div>

          <nav aria-label="Ana navigasyon" className="nav-list">
            {navigation.map(({ icon: Icon, label, active, count }) => (
              <button className={active ? "nav-item active" : "nav-item"} key={label} type="button">
                <Icon aria-hidden="true" size={18} strokeWidth={1.8} />
                <span>{label}</span>
                {count ? <span className="nav-count">{count}</span> : null}
              </button>
            ))}
          </nav>

          <div className="sidebar-bottom">
            <button className="nav-item" type="button"><Cloud size={18} />Bulut hesapları</button>
            <button className="nav-item" type="button"><ShieldCheck size={18} />Denetim izi</button>
            <button className="nav-item" type="button"><Settings size={18} />Ayarlar</button>
            <div className="hub-health"><span className="status-dot" />Hub çalışıyor <span>v0.1</span></div>
          </div>
        </aside>

        <main>
          <header className="topbar">
            <button className="search" type="button">
              <Search size={17} />
              <span>Sunucu, servis ve log ara…</span>
              <kbd><Command size={12} /> K</kbd>
            </button>
            <div className="topbar-actions">
              <Badge className="environment"><span className="status-dot" />Üretim</Badge>
              <ThemeToggle />
              <button aria-label="Bildirimler" className="icon-button" type="button"><Bell size={18} /></button>
              <div className="avatar">GB</div>
            </div>
          </header>

          <div className="content">
            <div className="page-heading">
              <div><p className="eyebrow">FİLO / ÜRETİM</p><h1>Operasyon özeti</h1></div>
              <div className="live-status"><span className="pulse" />Canlı · 8 sn önce güncellendi</div>
            </div>

            <section className="stat-grid" aria-label="Filo özeti">
              <Metric
                label="Sunucular"
                value={inventoryState === "loading" ? "—" : String(instances.length)}
                detail={inventoryState === "error" ? "Envantere ulaşılamıyor" : `${connectedInstances} bağlı`}
                trend={inventoryState === "ready" ? "Canlı envanter" : "Hub bekleniyor"}
              />
              <Metric label="Ortalama CPU" value="42.8%" detail="24 saatlik filo ortalaması" trend="düne göre −%3,2" />
              <Metric label="Açık alarmlar" value="3" detail="1 alarm ilgilenilmeyi bekliyor" trend="2 alarm onaylandı" alert />
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
                <div className="card-header"><div><h2>Operasyon alarmları</h2><p>İlgilenilmesi gereken sinyaller</p></div><Badge className="critical-count">3 açık</Badge></div>
                <div className="alert-list">
                  <Alert level="Kritik" title="CPU doygunluğu" host="worker-07" meta="12 dakikadır %91 · 6 dk önce" />
                  <Alert level="Uyarı" title="Servise ulaşılamıyor" host="worker-07" meta="queue-worker · 14 dk önce" />
                  <Alert level="Uyarı" title="Bellek baskısı" host="db-replica-02" meta="%79 ve yükseliyor · 28 dk önce" />
                </div>
                <button className="panel-link" type="button">Alarm merkezini aç <ChevronRight size={15} /></button>
              </Card>
            </div>

            <Card className="table-card">
              <div className="card-header">
                <div><h2>Sunucu envanteri</h2><p>Kayıtlı agent'ların raporladığı normalize edilmiş bilgiler</p></div>
                <button className="text-button" type="button">Tüm sunucuları görüntüle <ChevronRight size={15} /></button>
              </div>
              <div className="table-scroll">
                <table aria-label="Sunucu sağlığı">
                  <thead><tr><th>Sunucu</th><th>İşletim sistemi</th><th>Kapasite</th><th>Ağ</th><th>Agent</th><th>Son görülme</th><th>Durum</th><th aria-label="İşlemler" /></tr></thead>
                  <tbody>
                    {instances.map((instance) => (
                      <tr key={instance.agent_id}>
                        <td><div className="instance-cell"><span className="instance-icon"><Server size={17} /></span><span><strong>{instance.hostname}</strong><small>{instance.os_name} {instance.os_version}</small></span></div></td>
                        <td><span className="mono">{instance.os_family}</span><small className="cell-meta">{instance.kernel_version}</small></td>
                        <td><span className="mono">{instance.cpu_cores} çekirdek · {formatMemory(instance.memory_bytes)}</span><small className="cell-meta">{instance.architecture}</small></td>
                        <td className="mono muted">{instance.ip_addresses[0] ?? "Adres yok"}</td>
                        <td><span className="mono">v{instance.agent_version}</span><small className="cell-meta">{instance.agent_id.slice(0, 8)}</small></td>
                        <td className="mono muted">{formatLastSeen(instance.last_seen_at)}</td>
                        <td><Badge className={instance.status === "connected" ? "healthy" : "warning"}><span className="status-dot" />{instance.status === "connected" ? "Bağlı" : "Eski veri"}</Badge></td>
                        <td><button aria-label={`${instance.hostname} ayrıntılarını aç`} className="row-button" onClick={() => openTelemetry(instance)} type="button"><ChevronRight size={17} /></button></td>
                      </tr>
                    ))}
                    {inventoryState !== "loading" && instances.length === 0 ? (
                      <tr><td className="empty-table" colSpan={8}>{inventoryState === "error" ? "Envantere geçici olarak ulaşılamıyor." : "Kayıtlı hiçbir sunucu henüz rapor göndermedi."}</td></tr>
                    ) : null}
                    {inventoryState === "loading" ? <tr><td className="empty-table" colSpan={8}>Sunucu envanteri yükleniyor…</td></tr> : null}
                  </tbody>
                </table>
              </div>
            </Card>

            {selectedInstance ? (
              <>
                <TelemetryPanel
                  instance={selectedInstance}
                  onClose={() => setSelectedInstance(null)}
                  payload={telemetry}
                  state={telemetryState}
                />
                <ServicesPanel key={selectedInstance.agent_id} instance={selectedInstance} services={services} state={serviceState} />
              </>
            ) : null}
          </div>
        </main>
      </div>
    </div>
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

function formatMemory(bytes: number) {
  return `${Math.round(bytes / 1024 / 1024 / 1024)} GiB`
}

function formatLastSeen(value: string) {
  const timestamp = new Date(value)
  if (Number.isNaN(timestamp.getTime())) return "Bilinmiyor"
  return timestamp.toLocaleString("tr-TR", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
}

function TelemetryPanel({ instance, onClose, payload, state }: {
  instance: InventoryInstance
  onClose: () => void
  payload: TelemetryPayload | null
  state: "idle" | "loading" | "ready" | "error"
}) {
  const latest = payload?.latest
  return (
    <Card aria-label={`${instance.hostname} telemetrisi`} className="telemetry-card">
      <div className="card-header telemetry-header">
        <div><h2>Kaynak telemetrisi</h2><p>{instance.hostname} · son 24 saat</p></div>
        <button aria-label="Telemetri panelini kapat" className="icon-button" onClick={onClose} type="button"><X size={17} /></button>
      </div>
      {state === "loading" ? <div className="telemetry-state">Telemetri yükleniyor…</div> : null}
      {state === "error" ? <div className="telemetry-state">Telemetri şu anda alınamıyor.</div> : null}
      {state === "ready" && !latest ? <div className="telemetry-state">Bu sunucu henüz telemetri göndermedi.</div> : null}
      {latest ? (
        <div className="telemetry-content">
          <div className="telemetry-kpis">
            <TelemetryMetric label="CPU" value={`${latest.cpu_percent.toFixed(1)}%`} />
            <TelemetryMetric label="Bellek" value={`${latest.memory_percent.toFixed(1)}%`} />
            <TelemetryMetric label="Disk" value={`${latest.disk_percent.toFixed(1)}%`} />
            <TelemetryMetric label="Ağ RX / TX" value={`${formatBytes(latest.network_rx_bytes)} / ${formatBytes(latest.network_tx_bytes)}`} />
          </div>
          <TelemetryChart samples={payload?.samples ?? []} />
        </div>
      ) : null}
    </Card>
  )
}

function TelemetryMetric({ label, value }: { label: string; value: string }) {
  return <div className="telemetry-metric"><span>{label}</span><strong>{value}</strong></div>
}

function TelemetryChart({ samples }: { samples: TelemetrySample[] }) {
  return (
    <div className="telemetry-chart-wrap">
      <div className="chart-legend"><span className="cpu">CPU</span><span className="memory">Bellek</span></div>
      <svg aria-label="Son 24 saat CPU ve bellek kullanımı" className="telemetry-chart" role="img" viewBox="0 0 800 220">
        {[20, 65, 110, 155, 200].map((y) => <line className="grid-line" key={y} x1="0" x2="800" y1={y} y2={y} />)}
        <polyline className="memory-line" points={telemetryPoints(samples, "memory_percent")} />
        <polyline className="cpu-line" points={telemetryPoints(samples, "cpu_percent")} />
      </svg>
    </div>
  )
}

function telemetryPoints(samples: TelemetrySample[], key: "cpu_percent" | "memory_percent") {
  if (samples.length === 0) return ""
  return samples.map((sample, index) => {
    const x = samples.length === 1 ? 400 : (index / (samples.length - 1)) * 800
    const y = 200 - (sample[key] / 100) * 180
    return `${x.toFixed(1)},${y.toFixed(1)}`
  }).join(" ")
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`
}

type ServiceFilter = "all" | ManagedService["state"]

function ServicesPanel({ instance, services, state }: {
  instance: InventoryInstance
  services: ManagedService[]
  state: "idle" | "loading" | "ready" | "error"
}) {
  const [filter, setFilter] = useState<ServiceFilter>("all")
  const [query, setQuery] = useState("")
  const normalizedQuery = query.trim().toLocaleLowerCase("tr-TR")
  const visibleServices = services.filter((service) => {
    const matchesState = filter === "all" || service.state === filter
    const searchable = `${service.name} ${service.display_name}`.toLocaleLowerCase("tr-TR")
    return matchesState && (normalizedQuery === "" || searchable.includes(normalizedQuery))
  })
  const failedCount = services.filter((service) => service.state === "failed").length

  return (
    <Card aria-label={`${instance.hostname} servisleri`} className="services-card">
      <div className="card-header services-header">
        <div><h2>Servis envanteri</h2><p>{instance.hostname} · {services.length} servis · {failedCount} başarısız</p></div>
        <label className="service-search">
          <Search aria-hidden="true" size={15} />
          <span className="sr-only">Servislerde ara</span>
          <input aria-label="Servislerde ara" onChange={(event) => setQuery(event.target.value)} placeholder="Servis ara…" type="search" value={query} />
        </label>
      </div>
      <div aria-label="Servis durumu filtresi" className="service-filters" role="group">
        <ServiceFilterButton active={filter === "all"} label="Tümü" onClick={() => setFilter("all")} />
        <ServiceFilterButton active={filter === "running"} label="Çalışıyor" onClick={() => setFilter("running")} />
        <ServiceFilterButton active={filter === "stopped"} label="Durduruldu" onClick={() => setFilter("stopped")} />
        <ServiceFilterButton active={filter === "failed"} label="Başarısız" onClick={() => setFilter("failed")} accessibleLabel="Başarısız servisleri göster" />
      </div>
      {state === "loading" ? <div className="service-state">Servis envanteri yükleniyor…</div> : null}
      {state === "error" ? <div className="service-state">Servis envanterine şu anda ulaşılamıyor.</div> : null}
      {state === "ready" && visibleServices.length === 0 ? <div className="service-state">Bu filtreyle eşleşen servis yok.</div> : null}
      {state === "ready" && visibleServices.length > 0 ? (
        <div className="table-scroll">
          <table aria-label={`${instance.hostname} servis listesi`}>
            <thead><tr><th>Servis</th><th>Durum</th><th>Başlangıç</th><th>Son gözlem</th></tr></thead>
            <tbody>
              {visibleServices.map((service) => (
                <tr key={service.name}>
                  <td><strong className="service-name">{service.display_name || service.name}</strong><small className="cell-meta">{service.name}</small></td>
                  <td><Badge className={`service-badge ${service.state}`}><span className="status-dot" />{serviceStateLabel(service.state)}</Badge></td>
                  <td>{startupTypeLabel(service.startup_type)}</td>
                  <td className="mono muted">{formatLastSeen(service.observed_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </Card>
  )
}

function ServiceFilterButton({ active, label, onClick, accessibleLabel }: {
  active: boolean
  label: string
  onClick: () => void
  accessibleLabel?: string
}) {
  return <button aria-label={accessibleLabel} aria-pressed={active} className={active ? "service-filter active" : "service-filter"} onClick={onClick} type="button">{label}</button>
}

function serviceStateLabel(state: ManagedService["state"]) {
  return { running: "Çalışıyor", stopped: "Durduruldu", failed: "Başarısız", unknown: "Bilinmiyor" }[state]
}

function startupTypeLabel(startupType: ManagedService["startup_type"]) {
  return { automatic: "Otomatik", manual: "Manuel", disabled: "Devre dışı", unknown: "Bilinmiyor" }[startupType]
}
