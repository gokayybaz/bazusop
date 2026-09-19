import type { ReactNode } from "react"
import { Server } from "lucide-react"

import { Badge } from "./ui/badge"
import { Card } from "./ui/card"
import type { InventoryInstance } from "../types"

export function InstancePage({ instances, selected, onSelect, children }: { instances: InventoryInstance[]; selected: InventoryInstance | null; onSelect: (instance: InventoryInstance) => void; children: ReactNode }) {
  return <div className="instance-workspace"><Card aria-label="Sunucu seçimi" className="instance-picker"><div><strong>Sunucu seçin</strong><span>Bu sayfadaki veriler seçilen agent için gösterilir.</span></div><div className="instance-picker-list">{instances.map((instance) => <button aria-pressed={selected?.agent_id === instance.agent_id} className={selected?.agent_id === instance.agent_id ? "active" : ""} key={instance.agent_id} onClick={() => onSelect(instance)} type="button"><Server size={16} /><span><strong>{instance.hostname}</strong><small>{instance.os_name} {instance.os_version}</small></span><Badge className={instance.status === "connected" ? "healthy" : "warning"}>{instance.status === "connected" ? "Bağlı" : "Eski veri"}</Badge></button>)}</div>{instances.length === 0 ? <p className="picker-empty">Bu çalışma alanı için kayıtlı sunucu yok.</p> : null}</Card>{children}</div>
}
