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
} from "lucide-react"

import { ThemeToggle } from "./components/theme-toggle"
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
  { name: "web-prod-03", platform: "Ubuntu 24.04", zone: "eu-central-1a", cpu: 42, memory: 61, services: "18 / 18", state: "Healthy" },
  { name: "api-prod-01", platform: "Windows Server 2025", zone: "westeurope-2", cpu: 68, memory: 74, services: "31 / 31", state: "Healthy" },
  { name: "worker-07", platform: "Rocky Linux 9", zone: "on-prem / rack-4", cpu: 91, memory: 86, services: "13 / 14", state: "Warning" },
  { name: "db-replica-02", platform: "Debian 13", zone: "eu-central-1b", cpu: 36, memory: 79, services: "11 / 11", state: "Healthy" },
]

export function App() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <div className="app-frame">
        <aside className="sidebar">
          <div className="brand">
            <div className="brand-mark" aria-hidden="true">U</div>
            <div><strong>bazUSOP</strong><span>control plane</span></div>
          </div>

          <nav aria-label="Primary navigation" className="nav-list">
            {navigation.map(({ icon: Icon, label, active, count }) => (
              <button className={active ? "nav-item active" : "nav-item"} key={label} type="button">
                <Icon aria-hidden="true" size={18} strokeWidth={1.8} />
                <span>{label}</span>
                {count ? <span className="nav-count">{count}</span> : null}
              </button>
            ))}
          </nav>

          <div className="sidebar-bottom">
            <button className="nav-item" type="button"><Cloud size={18} />Cloud accounts</button>
            <button className="nav-item" type="button"><ShieldCheck size={18} />Audit trail</button>
            <button className="nav-item" type="button"><Settings size={18} />Settings</button>
            <div className="hub-health"><span className="status-dot" />Hub operational <span>v0.1</span></div>
          </div>
        </aside>

        <main>
          <header className="topbar">
            <button className="search" type="button">
              <Search size={17} />
              <span>Search instances, services, logs…</span>
              <kbd><Command size={12} /> K</kbd>
            </button>
            <div className="topbar-actions">
              <Badge className="environment"><span className="status-dot" />Production</Badge>
              <ThemeToggle />
              <button aria-label="Notifications" className="icon-button" type="button"><Bell size={18} /></button>
              <div className="avatar">GB</div>
            </div>
          </header>

          <div className="content">
            <div className="page-heading">
              <div><p className="eyebrow">FLEET / PRODUCTION</p><h1>Operations overview</h1></div>
              <div className="live-status"><span className="pulse" />Live · updated 8s ago</div>
            </div>

            <section className="stat-grid" aria-label="Fleet summary">
              <Metric label="Instances" value="24" detail="22 connected" trend="+2 this month" />
              <Metric label="Average CPU" value="42.8%" detail="24h fleet average" trend="−3.2% vs yesterday" />
              <Metric label="Open alerts" value="3" detail="1 needs attention" trend="2 acknowledged" alert />
            </section>

            <div className="dashboard-grid">
              <Card className="chart-card">
                <div className="card-header">
                  <div><h2>Fleet resource utilization</h2><p>CPU and memory · last 24 hours</p></div>
                  <div className="chart-legend"><span className="cpu">CPU</span><span className="memory">Memory</span></div>
                </div>
                <ResourceChart />
              </Card>

              <Card aria-label="Operational alerts" className="alerts-card">
                <div className="card-header"><div><h2>Operational alerts</h2><p>Signals that need attention</p></div><Badge className="critical-count">3 open</Badge></div>
                <div className="alert-list">
                  <Alert level="Critical" title="CPU saturation" host="worker-07" meta="91% for 12 min · 6 min ago" />
                  <Alert level="Warning" title="Service unavailable" host="worker-07" meta="queue-worker · 14 min ago" />
                  <Alert level="Warning" title="Memory pressure" host="db-replica-02" meta="79% and rising · 28 min ago" />
                </div>
                <button className="panel-link" type="button">Open alert center <ChevronRight size={15} /></button>
              </Card>
            </div>

            <Card className="table-card">
              <div className="card-header">
                <div><h2>Instance health</h2><p>Current load and service availability</p></div>
                <button className="text-button" type="button">View all instances <ChevronRight size={15} /></button>
              </div>
              <div className="table-scroll">
                <table aria-label="Instance health">
                  <thead><tr><th>Instance</th><th>Location</th><th>CPU</th><th>Memory</th><th>Services</th><th>Status</th><th aria-label="Actions" /></tr></thead>
                  <tbody>
                    {instances.map((instance) => (
                      <tr key={instance.name}>
                        <td><div className="instance-cell"><span className="instance-icon"><Server size={17} /></span><span><strong>{instance.name}</strong><small>{instance.platform}</small></span></div></td>
                        <td className="mono muted">{instance.zone}</td>
                        <td><Meter value={instance.cpu} warning={instance.cpu > 80} /></td>
                        <td><Meter value={instance.memory} warning={instance.memory > 80} /></td>
                        <td className="mono">{instance.services}</td>
                        <td><Badge className={instance.state === "Warning" ? "warning" : "healthy"}><span className="status-dot" />{instance.state}</Badge></td>
                        <td><button aria-label={`Open ${instance.name}`} className="row-button" type="button"><ChevronRight size={17} /></button></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
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
      <div className="metric-value"><strong>{value}</strong>{alert ? <span className="alert-pip">1 critical</span> : null}</div>
      <div className="metric-detail"><span>{detail}</span><span className={alert ? "trend alert" : "trend"}>{trend}</span></div>
    </Card>
  )
}

function ResourceChart() {
  return (
    <div className="chart-wrap">
      <div className="chart-scale"><span>100%</span><span>75%</span><span>50%</span><span>25%</span><span>0%</span></div>
      <svg aria-label="Fleet resource utilization over 24 hours" className="resource-chart" role="img" viewBox="0 0 800 250">
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
      <div className="chart-axis"><span>00:00</span><span>04:00</span><span>08:00</span><span>12:00</span><span>16:00</span><span>20:00</span><span>Now</span></div>
    </div>
  )
}

function Alert({ level, title, host, meta }: { level: "Critical" | "Warning"; title: string; host: string; meta: string }) {
  return (
    <div className={level === "Critical" ? "alert-item critical" : "alert-item warning-item"}>
      <CircleAlert size={18} />
      <div><span className="alert-level">{level}</span><strong>{title}</strong><span className="alert-host">{host}</span><small>{meta}</small></div>
    </div>
  )
}

function Meter({ value, warning = false }: { value: number; warning?: boolean }) {
  return <div className="meter"><div><i className={warning ? "hot" : ""} style={{ width: `${value}%` }} /></div><span className={warning ? "hot" : ""}>{value}%</span></div>
}
