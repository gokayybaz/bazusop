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
import { useEffect, useState, type ComponentType, type FormEvent, type ReactNode } from "react"

import { ThemeToggle } from "./components/theme-toggle"
import { Badge } from "./components/ui/badge"
import { Card } from "./components/ui/card"

type PageID = "overview" | "fleet" | "services" | "metrics" | "logs" | "jobs" | "alerts" | "cloud" | "audit" | "settings"

const navigation = [
  { id: "overview" as const, icon: Gauge, label: "Genel bakış", path: "/" },
  { id: "fleet" as const, icon: Server, label: "Filo", path: "/fleet" },
  { id: "services" as const, icon: Boxes, label: "Servisler", path: "/services" },
  { id: "metrics" as const, icon: ChartNoAxesCombined, label: "Metrikler", path: "/metrics" },
  { id: "logs" as const, icon: TerminalSquare, label: "Loglar", path: "/logs" },
  { id: "jobs" as const, icon: ListChecks, label: "İşler", path: "/jobs" },
  { id: "alerts" as const, icon: Bell, label: "Alarmlar", path: "/alerts" },
]

const secondaryNavigation = [
  { id: "cloud" as const, icon: Cloud, label: "Bulut hesapları", path: "/cloud" },
  { id: "audit" as const, icon: ShieldCheck, label: "Denetim izi", path: "/audit" },
  { id: "settings" as const, icon: Settings, label: "Ayarlar", path: "/settings" },
]

const pageMeta: Record<PageID, { eyebrow: string; title: string; description: string }> = {
  overview: { eyebrow: "FİLO / ÜRETİM", title: "Operasyon özeti", description: "Filo sağlığı ve ilgilenilmesi gereken sinyaller" },
  fleet: { eyebrow: "ENVANTER", title: "Sunucu filosu", description: "Kayıtlı Linux ve Windows agent'ları" },
  services: { eyebrow: "SUNUCU DURUMU", title: "Servisler", description: "systemd ve Windows Service envanteri" },
  metrics: { eyebrow: "GÖZLEMLENEBİLİRLİK", title: "Metrikler", description: "CPU, bellek, disk ve ağ telemetrisi" },
  logs: { eyebrow: "GÖZLEMLENEBİLİRLİK", title: "Loglar", description: "Geçmiş arama ve canlı log akışı" },
  jobs: { eyebrow: "OPERASYON", title: "İşler", description: "İmzalı ve denetlenebilir uzak aksiyonlar" },
  alerts: { eyebrow: "OLAY YÖNETİMİ", title: "Alarmlar", description: "Kurallar, olaylar ve bakım pencereleri" },
  cloud: { eyebrow: "KEŞİF", title: "Bulut hesapları", description: "AWS, Azure ve GCP envanter bağlantıları" },
  audit: { eyebrow: "YÖNETİŞİM", title: "Denetim izi", description: "Operasyon ve olay geçmişi" },
  settings: { eyebrow: "SİSTEM", title: "Ayarlar", description: "Hub ve arayüz tercihleri" },
}

function pageFromPath(pathname: string): PageID {
  return [...navigation, ...secondaryNavigation].find((item) => item.path === pathname)?.id ?? "overview"
}

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

type LogEntry = {
  id: string
  agent_id: string
  occurred_at: string
  collector: "journald" | "file" | "windows_event"
  source: string
  severity: "debug" | "info" | "warn" | "error" | "critical"
  message: string
}

type OperationJob = {
  id: string
  agent_id: string
  action: "service.restart" | "host.reboot"
  target: string
  approved_by: string
  reason: string
  requested_at: string
  status: "queued" | "running" | "succeeded" | "failed"
  last_sequence: number
  signature: string
  signing_public_key: string
}

type JobEvent = {
  job_id: string
  sequence: number
  type: "approved" | "claimed" | "output" | "succeeded" | "failed"
  message: string
  actor: string
  occurred_at: string
}

type AlertIncident = {
  id: string
  rule_id: string
  rule_name: string
  agent_id: string
  severity: "warning" | "critical"
  status: "open" | "acknowledged" | "resolved"
  message: string
  latest_value: number
  opened_at: string
  acknowledged_at?: string
  acknowledged_by?: string
  resolved_at?: string
}

type AlertRule = { id: string; name: string; kind: "metric" | "reachability"; metric: "cpu" | "memory" | "disk" | ""; threshold: number; stale_after_seconds: number; severity: "warning" | "critical"; enabled: boolean; created_at: string }
type MaintenanceWindow = { id: string; name: string; agent_id: string; starts_at: string; ends_at: string; created_by: string; created_at: string }
type CloudAccount = { id: string; name: string; provider: "aws" | "azure" | "gcp"; external_id: string; status: "pending" | "connected"; last_sync_at?: string }
type CloudInstance = { account_id: string; account_name: string; provider: CloudAccount["provider"]; provider_instance_id: string; name: string; region: string; zone?: string; state: string; os_family: "linux" | "windows" | "unknown"; private_ips: string[]; public_ips: string[]; agent_id_hint?: string; agent_id: string; candidate_agent_id?: string; match_status: "verified" | "candidate" | "unmatched"; match_reason: string }

export function App() {
  const [activePage, setActivePage] = useState<PageID>(() => pageFromPath(window.location.pathname))
  const [instances, setInstances] = useState<InventoryInstance[]>([])
  const [inventoryState, setInventoryState] = useState<"loading" | "ready" | "error">("loading")
  const [selectedInstance, setSelectedInstance] = useState<InventoryInstance | null>(null)
  const [telemetry, setTelemetry] = useState<TelemetryPayload | null>(null)
  const [telemetryState, setTelemetryState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [services, setServices] = useState<ManagedService[]>([])
  const [serviceState, setServiceState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [logState, setLogState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [jobs, setJobs] = useState<OperationJob[]>([])
  const [jobState, setJobState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [incidents, setIncidents] = useState<AlertIncident[]>([])
  const [alertState, setAlertState] = useState<"loading" | "ready" | "error">("loading")

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
    fetch("/api/v1/incidents?limit=100", { signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error("incidents unavailable")
        return response.json() as Promise<{ incidents: AlertIncident[] }>
      })
      .then((payload) => {
        setIncidents(payload.incidents ?? [])
        setAlertState("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setAlertState("error")
      })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    const syncRoute = () => setActivePage(pageFromPath(window.location.pathname))
    window.addEventListener("popstate", syncRoute)
    return () => window.removeEventListener("popstate", syncRoute)
  }, [])

  const connectedInstances = instances.filter((instance) => instance.status === "connected").length
  const activeIncidents = incidents.filter((incident) => incident.status !== "resolved")
  const criticalIncidents = activeIncidents.filter((incident) => incident.severity === "critical").length

  function navigate(page: PageID) {
    const item = [...navigation, ...secondaryNavigation].find((candidate) => candidate.id === page)
    if (!item) return
    window.history.pushState({}, "", item.path)
    setActivePage(page)
    window.scrollTo({ top: 0, behavior: "smooth" })
  }

  function openTelemetry(instance: InventoryInstance) {
    setSelectedInstance(instance)
    setTelemetry(null)
    setTelemetryState("loading")
    setServices([])
    setServiceState("loading")
    setLogs([])
    setLogState("loading")
    setJobs([])
    setJobState("loading")
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
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/logs?limit=200`)
      .then((response) => {
        if (!response.ok) throw new Error("logs unavailable")
        return response.json() as Promise<{ entries: LogEntry[] }>
      })
      .then((payload) => {
        setLogs(payload.entries)
        setLogState("ready")
      })
      .catch(() => setLogState("error"))
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/jobs?limit=50`)
      .then((response) => {
        if (!response.ok) throw new Error("jobs unavailable")
        return response.json() as Promise<{ jobs: OperationJob[] }>
      })
      .then((payload) => {
        setJobs(payload.jobs)
        setJobState("ready")
      })
      .catch(() => setJobState("error"))
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
            {navigation.map(({ id, icon: Icon, label }) => {
              const count = label === "Alarmlar" ? activeIncidents.length : undefined
              return (
              <button aria-current={activePage === id ? "page" : undefined} className={activePage === id ? "nav-item active" : "nav-item"} key={label} onClick={() => navigate(id)} type="button">
                <Icon aria-hidden="true" size={18} strokeWidth={1.8} />
                <span>{label}</span>
                {count !== undefined ? <span className="nav-count">{count}</span> : null}
              </button>
              )
            })}
          </nav>

          <div className="sidebar-bottom">
            {secondaryNavigation.map(({ id, icon: Icon, label }) => <button aria-current={activePage === id ? "page" : undefined} className={activePage === id ? "nav-item active" : "nav-item"} key={id} onClick={() => navigate(id)} type="button"><Icon size={18} /><span>{label}</span></button>)}
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
              <div><p className="eyebrow">{pageMeta[activePage].eyebrow}</p><h1>{pageMeta[activePage].title}</h1><p className="page-description">{pageMeta[activePage].description}</p></div>
              <div className="live-status"><span className="pulse" />Hub bağlantısı aktif</div>
            </div>

            {activePage === "overview" ? <>
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
                <button aria-label="Alarm merkezini aç" className="panel-link" onClick={() => navigate("alerts")} type="button">Alarm merkezini aç <ChevronRight size={15} /></button>
              </Card>
            </div>
            </> : null}

            {activePage === "alerts" ? <AlarmCenter incidents={incidents} onIncidentUpdated={(updated) => setIncidents((current) => current.map((incident) => incident.id === updated.id ? updated : incident))} /> : null}

            {activePage === "fleet" ? <Card className="table-card page-card">
              <div className="card-header">
                <div><h2>Sunucu envanteri</h2><p>Kayıtlı agent'ların raporladığı normalize edilmiş bilgiler</p></div>
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
                        <td><button aria-label={`${instance.hostname} metriklerini aç`} className="row-button" onClick={() => { openTelemetry(instance); navigate("metrics") }} type="button"><ChevronRight size={17} /></button></td>
                      </tr>
                    ))}
                    {inventoryState !== "loading" && instances.length === 0 ? (
                      <tr><td className="empty-table" colSpan={8}>{inventoryState === "error" ? "Envantere geçici olarak ulaşılamıyor." : "Kayıtlı hiçbir sunucu henüz rapor göndermedi."}</td></tr>
                    ) : null}
                    {inventoryState === "loading" ? <tr><td className="empty-table" colSpan={8}>Sunucu envanteri yükleniyor…</td></tr> : null}
                  </tbody>
                </table>
              </div>
            </Card> : null}

            {activePage === "metrics" ? <InstancePage instances={instances} onSelect={openTelemetry} selected={selectedInstance}>{selectedInstance ?
                <TelemetryPanel
                  instance={selectedInstance}
                  onClose={() => setSelectedInstance(null)}
                  payload={telemetry}
                  state={telemetryState}
                /> : null}</InstancePage> : null}
            {activePage === "services" ? <InstancePage instances={instances} onSelect={openTelemetry} selected={selectedInstance}>{selectedInstance ? <ServicesPanel key={selectedInstance.agent_id} instance={selectedInstance} services={services} state={serviceState} /> : null}</InstancePage> : null}
            {activePage === "logs" ? <InstancePage instances={instances} onSelect={openTelemetry} selected={selectedInstance}>{selectedInstance ? <LogPanel key={`${selectedInstance.agent_id}-logs`} entries={logs} instance={selectedInstance} state={logState} /> : null}</InstancePage> : null}
            {activePage === "jobs" ? <InstancePage instances={instances} onSelect={openTelemetry} selected={selectedInstance}>{selectedInstance ? <JobPanel
                  instance={selectedInstance}
                  jobs={jobs}
                  key={`${selectedInstance.agent_id}-jobs`}
                  onCreated={(job) => setJobs((current) => [job, ...current])}
                  state={jobState}
                /> : null}</InstancePage> : null}
            {activePage === "cloud" ? <CloudInventoryPage /> : null}
            {activePage === "audit" ? <EmptyFeature icon={ShieldCheck} title="Denetim kaynakları ayrıştırıldı" text="İş ve alarm olayları kendi sayfalarında tutuluyor; birleşik denetim zaman çizelgesi bu sayfada sunulacak." /> : null}
            {activePage === "settings" ? <SettingsPage /> : null}
          </div>
        </main>
      </div>
    </div>
  )
}

function InstancePage({ instances, selected, onSelect, children }: { instances: InventoryInstance[]; selected: InventoryInstance | null; onSelect: (instance: InventoryInstance) => void; children: ReactNode }) {
  return <div className="instance-workspace"><Card aria-label="Sunucu seçimi" className="instance-picker"><div><strong>Sunucu seçin</strong><span>Bu sayfadaki veriler seçilen agent için gösterilir.</span></div><div className="instance-picker-list">{instances.map((instance) => <button aria-pressed={selected?.agent_id === instance.agent_id} className={selected?.agent_id === instance.agent_id ? "active" : ""} key={instance.agent_id} onClick={() => onSelect(instance)} type="button"><Server size={16} /><span><strong>{instance.hostname}</strong><small>{instance.os_name} {instance.os_version}</small></span><Badge className={instance.status === "connected" ? "healthy" : "warning"}>{instance.status === "connected" ? "Bağlı" : "Eski veri"}</Badge></button>)}</div>{instances.length === 0 ? <p className="picker-empty">Bu çalışma alanı için kayıtlı sunucu yok.</p> : null}</Card>{children}</div>
}

function EmptyFeature({ icon: Icon, title, text }: { icon: ComponentType<{ size?: number }>; title: string; text: string }) {
  return <Card className="empty-feature page-card"><span><Icon size={24} /></span><h2>{title}</h2><p>{text}</p></Card>
}

function SettingsPage() {
  return <div className="settings-grid"><Card className="settings-card"><div><h2>Görünüm</h2><p>Operasyon yüzeyi için açık veya koyu temayı seçin.</p></div><ThemeToggle /></Card><Card className="settings-card"><div><h2>Hub çalışma modu</h2><p>Bu önizleme süreç içi bellek deposu ve yerel bağlantı kullanıyor.</p></div><Badge className="environment">Yerel önizleme</Badge></Card></div>
}

function CloudInventoryPage() {
  const [accounts, setAccounts] = useState<CloudAccount[]>([])
  const [instances, setInstances] = useState<CloudInstance[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error">("loading")

  useEffect(() => {
    const controller = new AbortController()
    Promise.all([
      fetch("/api/v1/cloud/accounts", { signal: controller.signal }).then((response) => response.ok ? response.json() as Promise<{ accounts: CloudAccount[] }> : Promise.reject()),
      fetch("/api/v1/cloud/instances", { signal: controller.signal }).then((response) => response.ok ? response.json() as Promise<{ instances: CloudInstance[] }> : Promise.reject()),
    ]).then(([accountPayload, instancePayload]) => {
      setAccounts(accountPayload.accounts ?? [])
      setInstances(instancePayload.instances ?? [])
      setState("ready")
    }).catch((error: unknown) => {
      if ((error as { name?: string }).name !== "AbortError") setState("error")
    })
    return () => controller.abort()
  }, [])

  const verified = instances.filter((instance) => instance.match_status === "verified").length
  const candidates = instances.filter((instance) => instance.match_status === "candidate").length

  if (state === "loading") return <Card className="empty-feature page-card"><span><Cloud size={24} /></span><h2>Bulut envanteri yükleniyor</h2><p>Provider hesapları ve uzlaştırma sonuçları alınıyor.</p></Card>
  if (state === "error") return <EmptyFeature icon={Cloud} title="Bulut envanterine ulaşılamıyor" text="Hub bağlantısını ve cloud inventory API durumunu kontrol edin." />
  if (accounts.length === 0) return <EmptyFeature icon={Cloud} title="Henüz bulut hesabı bağlı değil" text="AWS, Azure veya GCP hesabını operatör API'siyle bağladığınızda keşfedilen sunucular burada görünür." />

  return <div className="cloud-workspace">
    <section aria-label="Bulut keşif özeti" className="cloud-stat-grid">
      <Card className="cloud-stat"><span>Bağlı hesap</span><strong>{accounts.filter((account) => account.status === "connected").length}</strong><small>{accounts.length} hesap yapılandırıldı</small></Card>
      <Card className="cloud-stat"><span>Keşfedilen instance</span><strong>{instances.length}</strong><small>AWS, Azure ve GCP toplamı</small></Card>
      <Card className="cloud-stat"><span>Doğrulanmış eşleşme</span><strong>{verified}</strong><small>{candidates} inceleme adayı</small></Card>
    </section>

    <Card className="cloud-accounts page-card">
      <div className="card-header"><div><h2>Provider hesapları</h2><p>Keşif bağlantıları ve son senkronizasyon durumu</p></div></div>
      <div className="cloud-account-list">{accounts.map((account) => <div className="cloud-account" key={account.id}><span className={`provider-mark ${account.provider}`}>{providerLabel(account.provider).slice(0, 1)}</span><div><strong>{account.name}</strong><small>{providerLabel(account.provider)} · {account.external_id}</small></div><Badge className={account.status === "connected" ? "healthy" : "warning"}>{account.status === "connected" ? "Bağlı" : "İlk keşif bekleniyor"}</Badge><time>{account.last_sync_at ? formatLastSeen(account.last_sync_at) : "Henüz eşitlenmedi"}</time></div>)}</div>
    </Card>

    <Card className="table-card page-card">
      <div className="card-header"><div><h2>Bulut sunucuları</h2><p>Provider envanteri ile bazUSOP agent kimliği uzlaştırması</p></div></div>
      <div className="table-scroll"><table aria-label="Bulut sunucuları"><thead><tr><th>Instance</th><th>Provider / hesap</th><th>Konum</th><th>İşletim sistemi</th><th>Durum</th><th>Agent eşleşmesi</th></tr></thead><tbody>{instances.map((instance) => <tr key={`${instance.account_id}-${instance.provider_instance_id}`}><td><strong>{instance.name}</strong><small className="cell-meta mono">{instance.provider_instance_id}</small></td><td><span>{providerLabel(instance.provider)}</span><small className="cell-meta">{instance.account_name}</small></td><td><span className="mono">{instance.region}</span><small className="cell-meta">{instance.zone || instance.private_ips[0] || "—"}</small></td><td>{instance.os_family === "windows" ? "Windows" : instance.os_family === "linux" ? "Linux" : "Bilinmiyor"}</td><td><Badge className={instance.state === "running" ? "healthy" : "warning"}>{instance.state}</Badge></td><td><Badge className={`cloud-match ${instance.match_status}`}>{matchStatusLabel(instance.match_status)}</Badge><small className="cell-meta mono">{instance.agent_id || instance.candidate_agent_id || (instance.agent_id_hint ? `${instance.agent_id_hint} bulunamadı` : "Agent sinyali yok")}</small></td></tr>)}</tbody></table></div>
    </Card>
  </div>
}

function providerLabel(provider: CloudAccount["provider"]) {
  return { aws: "AWS", azure: "Azure", gcp: "Google Cloud" }[provider]
}

function matchStatusLabel(status: CloudInstance["match_status"]) {
  return { verified: "Doğrulandı", candidate: "İnceleme adayı", unmatched: "Eşleşmedi" }[status]
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

function AlarmCenter({ incidents, onIncidentUpdated }: { incidents: AlertIncident[]; onIncidentUpdated: (incident: AlertIncident) => void }) {
  const [rules, setRules] = useState<AlertRule[]>([])
  const [windows, setWindows] = useState<MaintenanceWindow[]>([])
  const [actor, setActor] = useState("")
  const [accessToken, setAccessToken] = useState("")
  const [ruleName, setRuleName] = useState("")
  const [ruleKind, setRuleKind] = useState<AlertRule["kind"]>("metric")
  const [metric, setMetric] = useState<AlertRule["metric"]>("cpu")
  const [threshold, setThreshold] = useState("90")
  const [staleAfter, setStaleAfter] = useState("300")
  const [severity, setSeverity] = useState<AlertRule["severity"]>("warning")
  const [windowName, setWindowName] = useState("")
  const [windowAgent, setWindowAgent] = useState("")
  const [windowStart, setWindowStart] = useState("")
  const [windowEnd, setWindowEnd] = useState("")
  const [message, setMessage] = useState("")

  useEffect(() => {
    Promise.all([
      fetch("/api/v1/alert-rules").then((response) => response.ok ? response.json() as Promise<{ rules: AlertRule[] }> : Promise.reject()),
      fetch("/api/v1/maintenance-windows").then((response) => response.ok ? response.json() as Promise<{ windows: MaintenanceWindow[] }> : Promise.reject()),
    ]).then(([rulePayload, windowPayload]) => { setRules(rulePayload.rules ?? []); setWindows(windowPayload.windows ?? []) }).catch(() => setMessage("Alarm yapılandırması yüklenemedi."))
  }, [])

  const mutationHeaders = () => ({ "Authorization": `Bearer ${accessToken}`, "Content-Type": "application/json" })

  function createRule(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setMessage("")
    fetch("/api/v1/alert-rules", { method: "POST", headers: mutationHeaders(), body: JSON.stringify({ name: ruleName, kind: ruleKind, metric: ruleKind === "metric" ? metric : "", threshold: ruleKind === "metric" ? Number(threshold) : 0, stale_after_seconds: ruleKind === "reachability" ? Number(staleAfter) : 0, severity, enabled: true }) })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<AlertRule> })
      .then((rule) => { setRules((current) => [...current, rule]); setRuleName(""); setMessage("Alarm kuralı kaydedildi.") })
      .catch(() => setMessage("Kural kaydedilemedi; alanları ve yönetici token'ını kontrol edin."))
  }

  function createWindow(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setMessage("")
    fetch("/api/v1/maintenance-windows", { method: "POST", headers: mutationHeaders(), body: JSON.stringify({ name: windowName, agent_id: windowAgent, starts_at: new Date(windowStart).toISOString(), ends_at: new Date(windowEnd).toISOString(), created_by: actor }) })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<MaintenanceWindow> })
      .then((window) => { setWindows((current) => [...current, window]); setWindowName(""); setMessage("Bakım penceresi kaydedildi.") })
      .catch(() => setMessage("Bakım penceresi kaydedilemedi; zamanları ve yetki bilgilerini kontrol edin."))
  }

  function acknowledge(incident: AlertIncident) {
    setMessage("")
    fetch(`/api/v1/incidents/${encodeURIComponent(incident.id)}/acknowledge`, { method: "POST", headers: mutationHeaders(), body: JSON.stringify({ actor }) })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<AlertIncident> })
      .then((updated) => { onIncidentUpdated(updated); setMessage("Olay onaylandı.") })
      .catch(() => setMessage("Olay onaylanamadı; operatör bilgilerini kontrol edin."))
  }

  return (
    <Card aria-label="Alarm merkezi" className="alarm-center" id="alarm-center">
      <div className="card-header alarm-center-header"><div><h2>Alarm merkezi</h2><p>Politika değişiklikleri yönetici, olay onayı operatör yetkisi ister.</p></div></div>
      <div className="alarm-credentials"><label><span>Operatör</span><input onChange={(event) => setActor(event.target.value)} placeholder="Ad veya kimlik" value={actor} /></label><label><span>Yetkili token</span><input aria-label="Yetkili token" autoComplete="current-password" onChange={(event) => setAccessToken(event.target.value)} placeholder="••••••••" type="password" value={accessToken} /></label>{message ? <p aria-live="polite">{message}</p> : null}</div>
      <div className="alarm-center-grid">
        <section><h3>Olaylar</h3><div className="incident-list">{incidents.length === 0 ? <p className="alarm-empty">Henüz olay yok.</p> : incidents.map((incident) => <div className={`incident-row ${incident.severity}`} key={incident.id}><div><Badge className={`incident-status ${incident.status}`}>{incidentStatusLabel(incident.status)}</Badge><strong>{incident.rule_name}</strong><span>{incident.agent_id}</span><small>{incident.message}</small></div>{incident.status === "open" ? <button aria-label={`${incident.id} olayını onayla`} disabled={!actor || !accessToken} onClick={() => acknowledge(incident)} type="button">Onayla</button> : null}</div>)}</div></section>
        <section><h3>Yeni kural</h3><form className="alarm-form" onSubmit={createRule}><label><span>Ad</span><input onChange={(event) => setRuleName(event.target.value)} required value={ruleName} /></label><label><span>Tür</span><select onChange={(event) => setRuleKind(event.target.value as AlertRule["kind"])} value={ruleKind}><option value="metric">Metrik eşiği</option><option value="reachability">Erişilebilirlik</option></select></label>{ruleKind === "metric" ? <><label><span>Metrik</span><select onChange={(event) => setMetric(event.target.value as AlertRule["metric"])} value={metric}><option value="cpu">CPU</option><option value="memory">Bellek</option><option value="disk">Disk</option></select></label><label><span>Eşik (%)</span><input max="100" min="1" onChange={(event) => setThreshold(event.target.value)} required type="number" value={threshold} /></label></> : <label><span>Raporsuz süre (sn)</span><input min="60" onChange={(event) => setStaleAfter(event.target.value)} required type="number" value={staleAfter} /></label>}<label><span>Önem</span><select onChange={(event) => setSeverity(event.target.value as AlertRule["severity"])} value={severity}><option value="warning">Uyarı</option><option value="critical">Kritik</option></select></label><button disabled={!actor || !accessToken} type="submit">Kuralı kaydet</button></form><div className="compact-list">{rules.map((rule) => <span key={rule.id}><strong>{rule.name}</strong><small>{rule.kind === "metric" ? `${rule.metric.toUpperCase()} > %${rule.threshold}` : `${rule.stale_after_seconds} sn raporsuz`}</small></span>)}</div></section>
        <section><h3>Bakım penceresi</h3><form className="alarm-form" onSubmit={createWindow}><label><span>Ad</span><input onChange={(event) => setWindowName(event.target.value)} required value={windowName} /></label><label><span>Agent ID (boş = tümü)</span><input onChange={(event) => setWindowAgent(event.target.value)} value={windowAgent} /></label><label><span>Başlangıç</span><input onChange={(event) => setWindowStart(event.target.value)} required type="datetime-local" value={windowStart} /></label><label><span>Bitiş</span><input onChange={(event) => setWindowEnd(event.target.value)} required type="datetime-local" value={windowEnd} /></label><button disabled={!actor || !accessToken} type="submit">Pencereyi kaydet</button></form><div className="compact-list">{windows.map((window) => <span key={window.id}><strong>{window.name}</strong><small>{window.agent_id || "Tüm agent'lar"} · {formatLastSeen(window.starts_at)}</small></span>)}</div></section>
      </div>
    </Card>
  )
}

function incidentStatusLabel(status: AlertIncident["status"]) { return { open: "Açık", acknowledged: "Onaylandı", resolved: "Çözüldü" }[status] }

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

type LogFilter = "all" | LogEntry["severity"]

function LogPanel({ entries, instance, state }: {
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

function formatLogTime(value: string) {
  const timestamp = new Date(value)
  if (Number.isNaN(timestamp.getTime())) return "--:--:--"
  return timestamp.toLocaleTimeString("tr-TR", { hour: "2-digit", minute: "2-digit", second: "2-digit" })
}

function logSeverityLabel(severity: LogEntry["severity"]) {
  return { debug: "DEBUG", info: "BİLGİ", warn: "UYARI", error: "HATA", critical: "KRİTİK" }[severity]
}

function JobPanel({ instance, jobs, onCreated, state }: {
  instance: InventoryInstance
  jobs: OperationJob[]
  onCreated: (job: OperationJob) => void
  state: "idle" | "loading" | "ready" | "error"
}) {
  const [creating, setCreating] = useState(false)
  const [action, setAction] = useState<OperationJob["action"]>("service.restart")
  const [target, setTarget] = useState("")
  const [approvedBy, setApprovedBy] = useState("")
  const [reason, setReason] = useState("")
  const [operatorToken, setOperatorToken] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState(false)
  const [selectedJob, setSelectedJob] = useState<OperationJob | null>(null)
  const [events, setEvents] = useState<JobEvent[]>([])
  const [eventState, setEventState] = useState<"idle" | "loading" | "ready" | "error">("idle")

  function createJob(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setSubmitError(false)
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/jobs`, {
      method: "POST",
      headers: { "Authorization": `Bearer ${operatorToken}`, "Content-Type": "application/json" },
      body: JSON.stringify({ action, target: action === "service.restart" ? target : "", approved_by: approvedBy, reason }),
    })
      .then((response) => {
        if (!response.ok) throw new Error("job creation failed")
        return response.json() as Promise<OperationJob>
      })
      .then((job) => {
        onCreated(job)
        setCreating(false)
        setTarget("")
        setReason("")
        setOperatorToken("")
      })
      .catch(() => setSubmitError(true))
      .finally(() => setSubmitting(false))
  }

  function openAudit(job: OperationJob) {
    setSelectedJob(job)
    setEvents([])
    setEventState("loading")
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/jobs/${encodeURIComponent(job.id)}/events`)
      .then((response) => {
        if (!response.ok) throw new Error("audit unavailable")
        return response.json() as Promise<{ events: JobEvent[] }>
      })
      .then((payload) => {
        setEvents(payload.events)
        setEventState("ready")
      })
      .catch(() => setEventState("error"))
  }

  return (
    <Card aria-label={`${instance.hostname} işleri`} className="jobs-card">
      <div className="card-header jobs-header">
        <div><h2>Operasyon işleri</h2><p>{instance.hostname} · imzalı ve denetlenebilir aksiyonlar</p></div>
        <button className="text-button job-create-toggle" onClick={() => setCreating((current) => !current)} type="button">{creating ? "İptal" : "Yeni iş oluştur"}</button>
      </div>
      {creating ? (
        <form className="job-form" onSubmit={createJob}>
          <label><span>Aksiyon</span><select onChange={(event) => setAction(event.target.value as OperationJob["action"])} value={action}><option value="service.restart">Servisi yeniden başlat</option><option value="host.reboot">Sunucuyu yeniden başlat</option></select></label>
          <label><span>Hedef servis</span><input disabled={action === "host.reboot"} onChange={(event) => setTarget(event.target.value)} placeholder="nginx.service" required={action === "service.restart"} value={target} /></label>
          <label><span>Onaylayan</span><input onChange={(event) => setApprovedBy(event.target.value)} placeholder="Operatör kimliği" required value={approvedBy} /></label>
          <label><span>Operatör token'ı</span><input autoComplete="current-password" onChange={(event) => setOperatorToken(event.target.value)} placeholder="••••••••" required type="password" value={operatorToken} /></label>
          <label className="job-reason"><span>Gerekçe</span><input onChange={(event) => setReason(event.target.value)} placeholder="Bu aksiyon neden gerekli?" required value={reason} /></label>
          <button className="job-submit" disabled={submitting} type="submit">{submitting ? "Sıraya alınıyor…" : "Onayı kaydet ve sıraya al"}</button>
          {submitError ? <p className="job-form-error">İş oluşturulamadı. Alanları ve hub bağlantısını kontrol edin.</p> : null}
        </form>
      ) : null}
      {state === "loading" ? <div className="service-state">İş geçmişi yükleniyor…</div> : null}
      {state === "error" ? <div className="service-state">İş geçmişine şu anda ulaşılamıyor.</div> : null}
      {state === "ready" && jobs.length === 0 ? <div className="service-state">Bu sunucu için henüz operasyon işi yok.</div> : null}
      {jobs.length > 0 ? (
        <div className="job-list">
          {jobs.map((job) => (
            <button aria-label={`${job.id} işinin audit kaydını aç`} className={selectedJob?.id === job.id ? "job-row active" : "job-row"} key={job.id} onClick={() => openAudit(job)} type="button">
              <span className="job-action"><ListChecks aria-hidden="true" size={17} /><span><strong>{jobActionLabel(job.action)}</strong><small>{job.target || instance.hostname}</small></span></span>
              <span className="job-approval"><strong>{job.approved_by}</strong><small>{job.reason}</small></span>
              <Badge className={`job-status ${job.status}`}>{jobStatusLabel(job.status)}</Badge>
              <time dateTime={job.requested_at}>{formatLastSeen(job.requested_at)}</time>
              <ChevronRight aria-hidden="true" size={16} />
            </button>
          ))}
        </div>
      ) : null}
      {selectedJob ? (
        <div className="job-audit">
          <div className="job-audit-heading"><strong>Audit kaydı</strong><span>{selectedJob.id}</span></div>
          {eventState === "loading" ? <div className="job-event-state">Olaylar yükleniyor…</div> : null}
          {eventState === "error" ? <div className="job-event-state">Audit kaydına ulaşılamıyor.</div> : null}
          {events.map((event) => (
            <div className={`job-event ${event.type}`} key={event.sequence}>
              <span className="job-event-sequence">#{event.sequence}</span>
              <span><strong>{jobEventLabel(event.type)}</strong><small>{event.actor} · {formatLastSeen(event.occurred_at)}</small></span>
              <code>{event.message}</code>
            </div>
          ))}
        </div>
      ) : null}
    </Card>
  )
}

function jobActionLabel(action: OperationJob["action"]) {
  return action === "service.restart" ? "Servisi yeniden başlat" : "Sunucuyu yeniden başlat"
}

function jobStatusLabel(status: OperationJob["status"]) {
  return { queued: "Sırada", running: "Çalışıyor", succeeded: "Başarılı", failed: "Başarısız" }[status]
}

function jobEventLabel(type: JobEvent["type"]) {
  return { approved: "Onaylandı", claimed: "Agent teslim aldı", output: "Çıktı", succeeded: "Tamamlandı", failed: "Başarısız" }[type]
}
