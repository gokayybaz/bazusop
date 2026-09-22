import { Bell, Command, Search } from "lucide-react"
import { useEffect, useState } from "react"

import { ThemeToggle } from "./components/theme-toggle"
import { Badge } from "./components/ui/badge"
import { InstancePage } from "./components/instance-page"
import { navigation, pageFromPath, pageMeta, secondaryNavigation } from "./navigation"
import { ActivityPage } from "./pages/activity"
import { AlarmCenter } from "./pages/alarm-center"
import { AuditTrailPage } from "./pages/audit"
import { CloudInventoryPage } from "./pages/cloud-inventory"
import { FleetPage } from "./pages/fleet"
import { JobPanel } from "./pages/job-panel"
import { LogPanel } from "./pages/log-panel"
import { OverviewPage } from "./pages/overview"
import { ServicesPanel } from "./pages/services-panel"
import { SettingsPage } from "./pages/settings"
import { TelemetryPanel } from "./pages/telemetry-panel"
import type { AlertIncident, InventoryInstance, LogEntry, ManagedService, OperationJob, PageID, TelemetryPayload } from "./types"

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

  const activeIncidents = incidents.filter((incident) => incident.status !== "resolved")

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

            {activePage === "overview" ? <OverviewPage activeIncidents={activeIncidents} alertState={alertState} instances={instances} inventoryState={inventoryState} onOpenAlarmCenter={() => navigate("alerts")} /> : null}

            {activePage === "alerts" ? <AlarmCenter incidents={incidents} onIncidentUpdated={(updated) => setIncidents((current) => current.map((incident) => incident.id === updated.id ? updated : incident))} /> : null}

            {activePage === "fleet" ? <FleetPage instances={instances} inventoryState={inventoryState} onOpenMetrics={(instance) => { openTelemetry(instance); navigate("metrics") }} /> : null}

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
            {activePage === "activity" ? <ActivityPage /> : null}
            {activePage === "audit" ? <AuditTrailPage /> : null}
            {activePage === "settings" ? <SettingsPage /> : null}
          </div>
        </main>
      </div>
    </div>
  )
}
