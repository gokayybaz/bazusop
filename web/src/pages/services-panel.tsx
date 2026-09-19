import { useState } from "react"
import { Search } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { ServiceFilterButton } from "../components/filter-button"
import { formatLastSeen } from "../lib/format"
import type { InventoryInstance, ManagedService } from "../types"

type ServiceFilter = "all" | ManagedService["state"]

export function ServicesPanel({ instance, services, state }: {
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

function serviceStateLabel(state: ManagedService["state"]) {
  return { running: "Çalışıyor", stopped: "Durduruldu", failed: "Başarısız", unknown: "Bilinmiyor" }[state]
}

function startupTypeLabel(startupType: ManagedService["startup_type"]) {
  return { automatic: "Otomatik", manual: "Manuel", disabled: "Devre dışı", unknown: "Bilinmiyor" }[startupType]
}
