import { useEffect, useState } from "react"
import { Cloud } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { formatLastSeen } from "../lib/format"
import type { CloudAccount, CloudInstance } from "../types"

export function CloudInventoryPage() {
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
