import {
  Activity,
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
} from "lucide-react"

import { Badge } from "./components/ui/badge"
import { Card } from "./components/ui/card"

const navigation = [
  { icon: Gauge, label: "Overview", active: true },
  { icon: Server, label: "Fleet" },
  { icon: Boxes, label: "Services" },
  { icon: ChartNoAxesCombined, label: "Metrics" },
  { icon: TerminalSquare, label: "Logs" },
  { icon: ListChecks, label: "Jobs" },
  { icon: Bell, label: "Alerts", count: 3 },
]

const instances = [
  { name: "web-prod-03", platform: "Ubuntu 24.04", zone: "eu-central-1a", load: 42, state: "Healthy" },
  { name: "api-prod-01", platform: "Windows Server 2025", zone: "westeurope-2", load: 68, state: "Healthy" },
  { name: "worker-07", platform: "Rocky Linux 9", zone: "on-prem / rack-4", load: 91, state: "Warning" },
]

export function App() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <div className="app-frame">
        <aside className="sidebar">
          <div className="brand">
            <div className="brand-mark" aria-hidden="true">U</div>
            <div>
              <strong>bazUSOP</strong>
              <span>control plane</span>
            </div>
          </div>

          <nav aria-label="Primary navigation" className="nav-list">
            {navigation.map(({ icon: Icon, label, active, count }) => (
              <button className={active ? "nav-item active" : "nav-item"} key={label} type="button">
                <Icon aria-hidden="true" size={17} strokeWidth={1.8} />
                <span>{label}</span>
                {count ? <span className="nav-count">{count}</span> : null}
              </button>
            ))}
          </nav>

          <div className="sidebar-bottom">
            <button className="nav-item" type="button"><Cloud size={17} />Cloud accounts</button>
            <button className="nav-item" type="button"><ShieldCheck size={17} />Audit trail</button>
            <button className="nav-item" type="button"><Settings size={17} />Settings</button>
            <div className="hub-health"><span className="status-dot" />Hub operational <span>v0.1</span></div>
          </div>
        </aside>

        <main>
          <header className="topbar">
            <button className="search" type="button">
              <Search size={16} />
              <span>Search instances, services, logs…</span>
              <kbd><Command size={11} /> K</kbd>
            </button>
            <div className="topbar-actions">
              <Badge className="environment"><span className="status-dot" />Production</Badge>
              <button aria-label="Notifications" className="icon-button" type="button"><Bell size={18} /></button>
              <div className="avatar">GB</div>
            </div>
          </header>

          <div className="content">
            <div className="page-heading">
              <div>
                <p className="eyebrow">THURSDAY · 10 SEP · 19:48 TRT</p>
                <h1>Operations overview</h1>
              </div>
              <div className="live-status"><span className="pulse" />Live · updated 8s ago</div>
            </div>

            <section className="stat-grid" aria-label="Fleet summary">
              <Metric label="Instances" value="24" detail="22 connected" trend="+2 this month" />
              <Metric label="Healthy systems" value="91.7%" detail="22 of 24" trend="within target" />
              <Metric label="Open alerts" value="3" detail="1 needs attention" trend="2 acknowledged" alert />
              <Metric label="Jobs today" value="38" detail="37 successful" trend="97.4% success" />
            </section>

            <div className="dashboard-grid">
              <Card className="fleet-card">
                <div className="card-header">
                  <div><h2>Fleet health</h2><p>Resource pressure across connected systems</p></div>
                  <button className="text-button" type="button">View fleet <ChevronRight size={14} /></button>
                </div>
                <div className="fleet-list">
                  {instances.map((instance) => (
                    <div className="instance-row" key={instance.name}>
                      <div className="instance-icon"><Server size={17} /></div>
                      <div className="instance-name"><strong>{instance.name}</strong><span>{instance.platform}</span></div>
                      <span className="zone">{instance.zone}</span>
                      <div className="load"><div><span>CPU</span><strong>{instance.load}%</strong></div><div className="bar"><i style={{ width: `${instance.load}%` }} /></div></div>
                      <Badge className={instance.state === "Warning" ? "warning" : "healthy"}><span className="status-dot" />{instance.state}</Badge>
                      <ChevronRight className="row-arrow" size={16} />
                    </div>
                  ))}
                </div>
              </Card>

              <Card className="activity-card">
                <div className="card-header"><div><h2>Recent activity</h2><p>Alerts and operational changes</p></div></div>
                <div className="timeline">
                  <Event icon={CircleAlert} tone="amber" title="CPU pressure detected" meta="worker-07 · 6 min ago" />
                  <Event icon={Activity} tone="blue" title="nginx restarted" meta="web-prod-03 · 18 min ago" />
                  <Event icon={ListChecks} tone="green" title="Patch job completed" meta="12 instances · 42 min ago" />
                </div>
                <button className="activity-link" type="button">Open activity timeline <ChevronRight size={14} /></button>
              </Card>
            </div>
          </div>
        </main>
      </div>
    </div>
  )
}

function Metric({ label, value, detail, trend, alert = false }: { label: string; value: string; detail: string; trend: string; alert?: boolean }) {
  return (
    <Card className="metric-card">
      <span className="metric-label">{label}</span>
      <div className="metric-value"><strong>{value}</strong>{alert ? <span className="alert-pip">1 critical</span> : null}</div>
      <div className="metric-detail"><span>{detail}</span><span className={alert ? "trend alert" : "trend"}>{trend}</span></div>
    </Card>
  )
}

function Event({ icon: Icon, tone, title, meta }: { icon: typeof Activity; tone: string; title: string; meta: string }) {
  return (
    <div className="event">
      <div className={`event-icon ${tone}`}><Icon size={15} /></div>
      <div><strong>{title}</strong><span>{meta}</span></div>
    </div>
  )
}

