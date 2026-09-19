import { X } from "lucide-react"

import { Card } from "../components/ui/card"
import { formatBytes } from "../lib/format"
import type { InventoryInstance, TelemetryPayload, TelemetrySample } from "../types"

export function TelemetryPanel({ instance, onClose, payload, state }: {
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
