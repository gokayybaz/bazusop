import { type FormEvent, useEffect, useState } from "react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { formatLastSeen } from "../lib/format"
import type { AlertIncident, AlertRule, MaintenanceWindow } from "../types"

export function AlarmCenter({ incidents, onIncidentUpdated }: { incidents: AlertIncident[]; onIncidentUpdated: (incident: AlertIncident) => void }) {
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
