import { ChevronRight, Server } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { formatLastSeen, formatMemory } from "../lib/format"
import type { InventoryInstance } from "../types"

export function FleetPage({ instances, inventoryState, onOpenMetrics }: {
  instances: InventoryInstance[]
  inventoryState: "loading" | "ready" | "error"
  onOpenMetrics: (instance: InventoryInstance) => void
}) {
  return (
    <Card className="table-card page-card">
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
                <td><button aria-label={`${instance.hostname} metriklerini aç`} className="row-button" onClick={() => onOpenMetrics(instance)} type="button"><ChevronRight size={17} /></button></td>
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
  )
}
