import { useEffect, useState } from "react"

import { ThemeToggle } from "../components/theme-toggle"
import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { formatBuildDate } from "../lib/format"
import type { RuntimeConfiguration } from "../types"

export function SettingsPage() {
  const [runtime, setRuntime] = useState<RuntimeConfiguration | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    fetch("/api/v1/system/configuration", { signal: controller.signal })
      .then((response) => response.ok ? response.json() as Promise<RuntimeConfiguration> : Promise.reject())
      .then(setRuntime)
      .catch(() => undefined)
    return () => controller.abort()
  }, [])
  const storageLabel = runtime?.storage === "postgresql" ? (runtime.timescale_enabled ? "TimescaleDB" : "PostgreSQL") : "Süreç içi bellek"
  return <div className="settings-grid"><Card className="settings-card"><div><h2>Görünüm</h2><p>Operasyon yüzeyi için açık veya koyu temayı seçin.</p></div><ThemeToggle /></Card><Card className="settings-card"><div><h2>Hub çalışma modu</h2><p>Etkin kalıcı depolama ve zaman serisi çalışma modu.</p></div><Badge className="environment">{runtime ? storageLabel : "Yükleniyor"}</Badge></Card><Card className="settings-card retention-card"><div><h2>Saklama politikası</h2><p>TimescaleDB etkin olduğunda otomatik uygulanır.</p></div><div className="retention-values"><span>Telemetri: {runtime?.telemetry_retention_days ?? "—"} gün</span><span>Loglar: {runtime?.log_retention_days ?? "—"} gün</span></div></Card><Card className="settings-card build-card"><div><h2>Hub sürümü</h2><p>Çalışan binary'nin sürüm ve kaynak kimliği.</p></div><div className="build-values"><strong>{runtime?.version ?? "—"}</strong><span>{runtime?.commit ?? "Yükleniyor"}</span><time dateTime={runtime?.build_date}>{runtime?.build_date ? formatBuildDate(runtime.build_date) : "—"}</time></div></Card></div>
}
