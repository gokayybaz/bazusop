# İşler ve Alarmlar Formlarını Oturum Tabanlı Hale Getirme (Spike 13.3) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** `job-panel.tsx` ve `alarm-center.tsx`'teki, 11.7'de kaldırılan eski operator/admin bearer köprüsünü varsayan "token yapıştır" alanlarını kaldırır; bu dört mutasyonu (iş oluşturma, alarm kuralı oluşturma, bakım penceresi oluşturma, olay onaylama) 13.1'in kurduğu oturum+CSRF altyapısına (`apiFetch`) bağlar. Bu formlar şu an tamamen bozuk — girilen herhangi bir "token" backend'de artık hiçbir anlam ifade etmiyor, her istek `401` alıyor.

**Mimari:** `SessionProvider` (13.1) zaten `app.tsx`'te tüm uygulamayı sarmalıyor; `JobPanel` ve `AlarmCenter` her ikisi de `AppShell` üzerinden bu ağacın içinde render edildiğinden, `useSession()` hook'unu doğrudan çağırıp `apiFetch`'i props olmadan tüketebilirler. Değişiklik yalnız bu iki dosyanın mutasyon çağrılarını ve formlarını kapsar — salt-okunur `fetch` çağrıları (iş audit kaydı, alarm kuralı/bakım penceresi listeleme) plan kapsamı dışıdır (13.1'in aynı ilkesi: GET istekleri CSRF gerektirmez).

**Teknoloji yığını:** React 19 + TypeScript, mevcut `SessionProvider`/`apiFetch` (13.1'de kuruldu).

**Spec:** [docs/superpowers/specs/2026-09-22-frontend-identity-rbac-ui-design.md](../specs/2026-09-22-frontend-identity-rbac-ui-design.md) — bu plan yalnız Spike 13.3'ü kapsar.

## Genel Kısıtlar

- **"Operatör"/"Onaylayan" gibi serbest metin aktör alanları DEĞİŞMEZ** — bunlar backend'in `approved_by`/`created_by`/`actor` alanlarına karşılık gelen, kimlik doğrulamadan bağımsız iş alanlarıdır (backend bunları hâlâ serbest string olarak kabul ediyor). Bu plan yalnız kaldırılan bearer-token KİMLİK DOĞRULAMA alanlarını (`Operatör token'ı`, `Yetkili token`) kaldırır; oturum açmış kullanıcının e-postasıyla bu iş alanlarını otomatik doldurmak ayrı, kapsam dışı bir UX kararı olurdu.
- **Salt-okunur `fetch` çağrıları `apiFetch`'e taşınmaz** — `JobPanel`'in audit kaydı çekme çağrısı, `AlarmCenter`'ın kural/pencere listeleme çağrıları GET'tir, CSRF gerektirmez, plan kapsamı dışıdır (13.1'in Genel Kısıtlar'ındaki aynı ilke).
- **`job-panel.tsx`/`alarm-center.tsx` için ayrı, izole test dosyaları oluşturulur** (`app.test.tsx`'e eklenmez) — bu iki bileşen zaten `useSession()`'ı bağımsız tüketebiliyor, `SessionProvider` ile sarmalanıp kendi başlarına test edilebilirler; bu, `13.1`'in `bootstrap.test.tsx`/`invite.test.tsx`'te izlediği desenle tutarlıdır ve `app.test.tsx`'in daha da şişmesini önler. `app.test.tsx`'teki mevcut "shows managed alarm incidents..." testi yalnız artık var olmayan "Yetkili token" alanına dair iddiasını kaybeder, geri kalanı (route/render entegrasyonu) korunur.

## Dosya Yapısı

- Değiştir: `web/src/pages/job-panel.tsx`, `web/src/pages/alarm-center.tsx`, `web/src/app.test.tsx`.
- Oluştur: `web/src/pages/job-panel.test.tsx`, `web/src/pages/alarm-center.test.tsx`.

## Görev 1: `job-panel.tsx`'i oturum tabanlı hale getir

**Dosyalar:**
- Değiştir: `web/src/pages/job-panel.tsx`
- Oluştur: `web/src/pages/job-panel.test.tsx`

**Arayüzler:**
- Tüketir: `useSession` (`../lib/session`) — `apiFetch` için.

- [ ] **Adım 1: `web/src/pages/job-panel.tsx`'i güncelle**

`import` bloğuna ekle:

```tsx
import { useSession } from "../lib/session"
```

`operatorToken` state'ini ve kullanıldığı yerleri kaldır; `createJob`'ı `apiFetch` kullanacak şekilde güncelle:

```tsx
export function JobPanel({ instance, jobs, onCreated, state }: {
  instance: InventoryInstance
  jobs: OperationJob[]
  onCreated: (job: OperationJob) => void
  state: "idle" | "loading" | "ready" | "error"
}) {
  const { apiFetch } = useSession()
  const [creating, setCreating] = useState(false)
  const [action, setAction] = useState<OperationJob["action"]>("service.restart")
  const [target, setTarget] = useState("")
  const [approvedBy, setApprovedBy] = useState("")
  const [reason, setReason] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState(false)
  const [selectedJob, setSelectedJob] = useState<OperationJob | null>(null)
  const [events, setEvents] = useState<JobEvent[]>([])
  const [eventState, setEventState] = useState<"idle" | "loading" | "ready" | "error">("idle")

  function createJob(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setSubmitError(false)
    apiFetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/jobs`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action, target: action === "service.restart" ? target : "", approved_by: approvedBy, reason }),
    })
      .then((response) => {
        if (!response.ok) throw new Error("job creation failed")
        return response.json() as Promise<OperationJob>
      })
      .then((job) => {
        onCreated(job)
        setCreating(false)
        setTarget("")
        setReason("")
      })
      .catch(() => setSubmitError(true))
      .finally(() => setSubmitting(false))
  }
```

(`openAudit`'ın gövdesi değişmeden kalır — hâlâ salt-okunur `fetch` kullanır.)

Formdan "Operatör token'ı" alanını kaldır:

```tsx
      {creating ? (
        <form className="job-form" onSubmit={createJob}>
          <label><span>Aksiyon</span><select onChange={(event) => setAction(event.target.value as OperationJob["action"])} value={action}><option value="service.restart">Servisi yeniden başlat</option><option value="host.reboot">Sunucuyu yeniden başlat</option></select></label>
          <label><span>Hedef servis</span><input disabled={action === "host.reboot"} onChange={(event) => setTarget(event.target.value)} placeholder="nginx.service" required={action === "service.restart"} value={target} /></label>
          <label><span>Onaylayan</span><input onChange={(event) => setApprovedBy(event.target.value)} placeholder="Operatör kimliği" required value={approvedBy} /></label>
          <label className="job-reason"><span>Gerekçe</span><input onChange={(event) => setReason(event.target.value)} placeholder="Bu aksiyon neden gerekli?" required value={reason} /></label>
          <button className="job-submit" disabled={submitting} type="submit">{submitting ? "Sıraya alınıyor…" : "Onayı kaydet ve sıraya al"}</button>
          {submitError ? <p className="job-form-error">İş oluşturulamadı. Alanları ve hub bağlantısını kontrol edin.</p> : null}
        </form>
      ) : null}
```

(Yalnız `<label><span>Operatör token'ı</span>...</label>` satırı kaldırıldı; diğer her şey aynı.)

- [ ] **Adım 2: Build/tip kontrolünü doğrula (test dosyası henüz yazılmadı)**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx tsc -b --noEmit 2>&1 | tail -40`
Beklenen: hata yok

- [ ] **Adım 3: `web/src/pages/job-panel.test.tsx`'i yaz**

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { JobPanel } from "./job-panel"
import { SessionProvider } from "../lib/session"
import type { InventoryInstance, OperationJob } from "../types"

const instance: InventoryInstance = {
  agent_id: "agent-01", hostname: "edge-01.example.com", os_family: "linux", os_name: "Ubuntu", os_version: "24.04",
  architecture: "amd64", kernel_version: "6.8.0", cpu_cores: 4, memory_bytes: 8589934592, ip_addresses: ["10.0.0.8"],
  agent_version: "0.3.0", first_seen_at: "2026-09-11T04:00:00Z", last_seen_at: "2026-09-11T04:05:00Z", status: "connected",
}

const authenticatedWhoAmI = { user_id: "user-1", email: "operator@example.com", role: "operator", csrf_token: "csrf-token-abc", expires_at: "2026-09-22T22:00:00Z" }

describe("JobPanel", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("has no operator-token field and submits job creation with the session's CSRF header", async () => {
    const createdJob: OperationJob = {
      id: "job-01", agent_id: "agent-01", action: "service.restart", target: "nginx.service", approved_by: "gokay",
      reason: "config rollout", requested_at: "2026-09-11T04:06:00Z", status: "queued", last_sequence: 0, signature: "", signing_public_key: "",
    }
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/jobs")) return Promise.resolve({ ok: true, json: async () => createdJob } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    const onCreated = vi.fn()
    render(
      <SessionProvider>
        <JobPanel instance={instance} jobs={[]} onCreated={onCreated} state="ready" />
      </SessionProvider>,
    )
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/session"))

    fireEvent.click(screen.getByRole("button", { name: "Yeni iş oluştur" }))
    expect(screen.queryByLabelText("Operatör token'ı")).not.toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText("nginx.service"), { target: { value: "nginx.service" } })
    fireEvent.change(screen.getByPlaceholderText("Operatör kimliği"), { target: { value: "gokay" } })
    fireEvent.change(screen.getByPlaceholderText("Bu aksiyon neden gerekli?"), { target: { value: "config rollout" } })
    fireEvent.click(screen.getByRole("button", { name: "Onayı kaydet ve sıraya al" }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(createdJob))
    const jobCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/jobs"))
    expect(jobCall).toBeDefined()
    const headers = new Headers((jobCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
    expect(headers.has("Authorization")).toBe(false)
  })
})
```

- [ ] **Adım 4: Testi çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx vitest run job-panel.test 2>&1 | tail -60`
Beklenen: BAŞARILI

- [ ] **Adım 5: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/pages/job-panel.tsx web/src/pages/job-panel.test.tsx
git commit -m "fix: create jobs via the session's CSRF-protected apiFetch, not a pasted bearer token"
```

## Görev 2: `alarm-center.tsx`'i oturum tabanlı hale getir

**Dosyalar:**
- Değiştir: `web/src/pages/alarm-center.tsx`
- Oluştur: `web/src/pages/alarm-center.test.tsx`

**Arayüzler:**
- Tüketir: `useSession` (`../lib/session`) — `apiFetch` için.

- [ ] **Adım 1: `web/src/pages/alarm-center.tsx`'i güncelle**

```tsx
import { type FormEvent, useEffect, useState } from "react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { formatLastSeen } from "../lib/format"
import { useSession } from "../lib/session"
import type { AlertIncident, AlertRule, MaintenanceWindow } from "../types"

const jsonHeaders = { "Content-Type": "application/json" }

export function AlarmCenter({ incidents, onIncidentUpdated }: { incidents: AlertIncident[]; onIncidentUpdated: (incident: AlertIncident) => void }) {
  const { apiFetch } = useSession()
  const [rules, setRules] = useState<AlertRule[]>([])
  const [windows, setWindows] = useState<MaintenanceWindow[]>([])
  const [actor, setActor] = useState("")
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

  function createRule(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setMessage("")
    apiFetch("/api/v1/alert-rules", { method: "POST", headers: jsonHeaders, body: JSON.stringify({ name: ruleName, kind: ruleKind, metric: ruleKind === "metric" ? metric : "", threshold: ruleKind === "metric" ? Number(threshold) : 0, stale_after_seconds: ruleKind === "reachability" ? Number(staleAfter) : 0, severity, enabled: true }) })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<AlertRule> })
      .then((rule) => { setRules((current) => [...current, rule]); setRuleName(""); setMessage("Alarm kuralı kaydedildi.") })
      .catch(() => setMessage("Kural kaydedilemedi; alanları kontrol edin."))
  }

  function createWindow(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setMessage("")
    apiFetch("/api/v1/maintenance-windows", { method: "POST", headers: jsonHeaders, body: JSON.stringify({ name: windowName, agent_id: windowAgent, starts_at: new Date(windowStart).toISOString(), ends_at: new Date(windowEnd).toISOString(), created_by: actor }) })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<MaintenanceWindow> })
      .then((window) => { setWindows((current) => [...current, window]); setWindowName(""); setMessage("Bakım penceresi kaydedildi.") })
      .catch(() => setMessage("Bakım penceresi kaydedilemedi; zamanları kontrol edin."))
  }

  function acknowledge(incident: AlertIncident) {
    setMessage("")
    apiFetch(`/api/v1/incidents/${encodeURIComponent(incident.id)}/acknowledge`, { method: "POST", headers: jsonHeaders, body: JSON.stringify({ actor }) })
      .then((response) => { if (!response.ok) throw new Error(); return response.json() as Promise<AlertIncident> })
      .then((updated) => { onIncidentUpdated(updated); setMessage("Olay onaylandı.") })
      .catch(() => setMessage("Olay onaylanamadı; operatör bilgilerini kontrol edin."))
  }

  return (
    <Card aria-label="Alarm merkezi" className="alarm-center" id="alarm-center">
      <div className="card-header alarm-center-header"><div><h2>Alarm merkezi</h2><p>Politika değişiklikleri yönetici, olay onayı operatör yetkisi ister.</p></div></div>
      <div className="alarm-credentials"><label><span>Operatör</span><input onChange={(event) => setActor(event.target.value)} placeholder="Ad veya kimlik" value={actor} /></label>{message ? <p aria-live="polite">{message}</p> : null}</div>
      <div className="alarm-center-grid">
        <section><h3>Olaylar</h3><div className="incident-list">{incidents.length === 0 ? <p className="alarm-empty">Henüz olay yok.</p> : incidents.map((incident) => <div className={`incident-row ${incident.severity}`} key={incident.id}><div><Badge className={`incident-status ${incident.status}`}>{incidentStatusLabel(incident.status)}</Badge><strong>{incident.rule_name}</strong><span>{incident.agent_id}</span><small>{incident.message}</small></div>{incident.status === "open" ? <button aria-label={`${incident.id} olayını onayla`} disabled={!actor} onClick={() => acknowledge(incident)} type="button">Onayla</button> : null}</div>)}</div></section>
        <section><h3>Yeni kural</h3><form className="alarm-form" onSubmit={createRule}><label><span>Ad</span><input onChange={(event) => setRuleName(event.target.value)} required value={ruleName} /></label><label><span>Tür</span><select onChange={(event) => setRuleKind(event.target.value as AlertRule["kind"])} value={ruleKind}><option value="metric">Metrik eşiği</option><option value="reachability">Erişilebilirlik</option></select></label>{ruleKind === "metric" ? <><label><span>Metrik</span><select onChange={(event) => setMetric(event.target.value as AlertRule["metric"])} value={metric}><option value="cpu">CPU</option><option value="memory">Bellek</option><option value="disk">Disk</option></select></label><label><span>Eşik (%)</span><input max="100" min="1" onChange={(event) => setThreshold(event.target.value)} required type="number" value={threshold} /></label></> : <label><span>Raporsuz süre (sn)</span><input min="60" onChange={(event) => setStaleAfter(event.target.value)} required type="number" value={staleAfter} /></label>}<label><span>Önem</span><select onChange={(event) => setSeverity(event.target.value as AlertRule["severity"])} value={severity}><option value="warning">Uyarı</option><option value="critical">Kritik</option></select></label><button disabled={!actor} type="submit">Kuralı kaydet</button></form><div className="compact-list">{rules.map((rule) => <span key={rule.id}><strong>{rule.name}</strong><small>{rule.kind === "metric" ? `${rule.metric.toUpperCase()} > %${rule.threshold}` : `${rule.stale_after_seconds} sn raporsuz`}</small></span>)}</div></section>
        <section><h3>Bakım penceresi</h3><form className="alarm-form" onSubmit={createWindow}><label><span>Ad</span><input onChange={(event) => setWindowName(event.target.value)} required value={windowName} /></label><label><span>Agent ID (boş = tümü)</span><input onChange={(event) => setWindowAgent(event.target.value)} value={windowAgent} /></label><label><span>Başlangıç</span><input onChange={(event) => setWindowStart(event.target.value)} required type="datetime-local" value={windowStart} /></label><label><span>Bitiş</span><input onChange={(event) => setWindowEnd(event.target.value)} required type="datetime-local" value={windowEnd} /></label><button disabled={!actor} type="submit">Pencereyi kaydet</button></form><div className="compact-list">{windows.map((window) => <span key={window.id}><strong>{window.name}</strong><small>{window.agent_id || "Tüm agent'lar"} · {formatLastSeen(window.starts_at)}</small></span>)}</div></section>
      </div>
    </Card>
  )
}

function incidentStatusLabel(status: AlertIncident["status"]) { return { open: "Açık", acknowledged: "Onaylandı", resolved: "Çözüldü" }[status] }
```

- [ ] **Adım 2: Build/tip kontrolünü doğrula**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx tsc -b --noEmit 2>&1 | tail -40`
Beklenen: hata yok

- [ ] **Adım 3: `web/src/pages/alarm-center.test.tsx`'i yaz**

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { AlarmCenter } from "./alarm-center"
import { SessionProvider } from "../lib/session"
import type { AlertIncident, AlertRule } from "../types"

const authenticatedWhoAmI = { user_id: "user-1", email: "operator@example.com", role: "operator", csrf_token: "csrf-token-abc", expires_at: "2026-09-22T22:00:00Z" }

const openIncident: AlertIncident = {
  id: "incident-01", rule_id: "rule-01", rule_name: "Disk kritik eşiği", agent_id: "db-01", severity: "critical",
  status: "open", message: "Disk 96.0%; eşik 90.0%", latest_value: 96, opened_at: "2026-09-11T08:00:00Z",
}

function stubBaseFetch(overrides: (url: string, init?: RequestInit) => Response | Promise<Response> | undefined) {
  return vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const overridden = overrides(url, init)
    if (overridden) return Promise.resolve(overridden)
    if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
    if (url.endsWith("/api/v1/alert-rules")) return Promise.resolve({ ok: true, json: async () => ({ rules: [] }) } as Response)
    if (url.endsWith("/api/v1/maintenance-windows")) return Promise.resolve({ ok: true, json: async () => ({ windows: [] }) } as Response)
    return Promise.resolve({ ok: false } as Response)
  })
}

describe("AlarmCenter", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("has no access-token field and acknowledges an incident with the session's CSRF header", async () => {
    const acknowledged: AlertIncident = { ...openIncident, status: "acknowledged", acknowledged_by: "gokay" }
    const fetchMock = stubBaseFetch((url) => {
      if (url.endsWith("/acknowledge")) return { ok: true, json: async () => acknowledged } as Response
      return undefined
    })
    vi.stubGlobal("fetch", fetchMock)

    const onIncidentUpdated = vi.fn()
    render(
      <SessionProvider>
        <AlarmCenter incidents={[openIncident]} onIncidentUpdated={onIncidentUpdated} />
      </SessionProvider>,
    )
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/session"))

    expect(screen.queryByLabelText("Yetkili token")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "incident-01 olayını onayla" })).toBeDisabled()

    fireEvent.change(screen.getByPlaceholderText("Ad veya kimlik"), { target: { value: "gokay" } })
    expect(screen.getByRole("button", { name: "incident-01 olayını onayla" })).toBeEnabled()
    fireEvent.click(screen.getByRole("button", { name: "incident-01 olayını onayla" }))

    await waitFor(() => expect(onIncidentUpdated).toHaveBeenCalledWith(acknowledged))
    const ackCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/acknowledge"))
    expect(ackCall).toBeDefined()
    const headers = new Headers((ackCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
    expect(headers.has("Authorization")).toBe(false)
  })

  it("creates an alert rule with the session's CSRF header", async () => {
    const createdRule: AlertRule = { id: "rule-99", name: "Yüksek CPU", kind: "metric", metric: "cpu", threshold: 90, stale_after_seconds: 0, severity: "critical", enabled: true, created_at: "2026-09-22T22:00:00Z" }
    const fetchMock = stubBaseFetch((url, init) => {
      if (url.endsWith("/api/v1/alert-rules") && init?.method === "POST") return { ok: true, json: async () => createdRule } as Response
      return undefined
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <AlarmCenter incidents={[]} onIncidentUpdated={() => undefined} />
      </SessionProvider>,
    )
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/session"))

    fireEvent.change(screen.getByPlaceholderText("Ad veya kimlik"), { target: { value: "gokay" } })
    // "Ad" appears twice (rule form and maintenance-window form both have a
    // field with that label) — getAllByLabelText avoids the ambiguity
    // getByLabelText would raise; the rule form is first in DOM order.
    fireEvent.change(screen.getAllByLabelText("Ad")[0], { target: { value: "Yüksek CPU" } })
    fireEvent.click(screen.getByRole("button", { name: "Kuralı kaydet" }))

    await waitFor(() => expect(screen.getByText("Yüksek CPU")).toBeInTheDocument())
    const createCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/api/v1/alert-rules") && (call[1] as RequestInit)?.method === "POST")
    expect(createCall).toBeDefined()
    const headers = new Headers((createCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })
})
```

- [ ] **Adım 4: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx vitest run alarm-center.test 2>&1 | tail -80`
Beklenen: 2 test BAŞARILI

- [ ] **Adım 5: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/pages/alarm-center.tsx web/src/pages/alarm-center.test.tsx
git commit -m "fix: manage alerts via the session's CSRF-protected apiFetch, not a pasted bearer token"
```

## Görev 3: `app.test.tsx`'i güncelle ve tam süiti doğrula

**Dosyalar:**
- Değiştir: `web/src/app.test.tsx`

- [ ] **Adım 1: "shows managed alarm incidents and opens the alarm center" testinden kaldırılan alana dair iddiayı sil**

`expect(screen.getByLabelText("Yetkili token")).toBeInTheDocument()` satırını testten kaldır — bu alan artık DOM'da yok. Testin geri kalanı (route entegrasyonu, olay listesi, onayla düğmesinin varlığı) değişmeden kalır.

- [ ] **Adım 2: Tam frontend test süitini ve production build'i çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm test 2>&1 | tail -50 && npm run build 2>&1 | tail -30`
Beklenen: tüm testler BAŞARILI, build BAŞARILI

- [ ] **Adım 3: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/app.test.tsx
git commit -m "test: drop the removed access-token assertion from the alarm center integration test"
```

## Görev 4: Docker Compose ile manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [ ] **Adım 1: Docker Compose ile hub'ı yeniden derleyip ayağa kaldır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && docker compose down -v >/dev/null 2>&1; BAZUSOP_PORT=8090 BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper docker compose up --build -d 2>&1 | tail -30`

- [ ] **Adım 2: Sağlık kontrolünü bekle**

Çalıştır: `for i in $(seq 1 20); do curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8090/api/v1/health | grep -q 200 && echo healthy && break; sleep 1; done`

- [ ] **Adım 3: Bootstrap ol, giriş yap, gerçek bir iş oluştur — tarayıcı benzeri bir akışla (curl + Python), eski token alanı hiç kullanmadan**

```bash
python3 -c "
import base64, hashlib, hmac, http.cookiejar, json, struct, time, urllib.parse, urllib.request

cookie_jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookie_jar))

def call(method, path, body=None, extra_headers=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {'Content-Type': 'application/json'}
    headers.update(extra_headers or {})
    request = urllib.request.Request('http://127.0.0.1:8090' + path, data=data, headers=headers, method=method)
    with opener.open(request) as response:
        return json.load(response)

def totp_code(secret_b32, at=None):
    at = at or time.time()
    key = base64.b32decode(secret_b32 + '=' * ((8 - len(secret_b32) % 8) % 8))
    counter = struct.pack('>Q', int(at // 30))
    digest = hmac.new(key, counter, hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    code = (struct.unpack('>I', digest[offset:offset+4])[0] & 0x7fffffff) % 1000000
    return f'{code:06d}'

def secret_from_uri(uri):
    return urllib.parse.parse_qs(urllib.parse.urlparse(uri).query)['secret'][0]

bootstrap_payload = call('POST', '/api/v1/bootstrap', {'secret': 'verify-bootstrap', 'email': 'admin@example.com', 'password': 'correct horse battery staple'})
admin_secret = secret_from_uri(bootstrap_payload['provisioning_uri'])
login_payload = call('POST', '/api/v1/sessions', {'email': 'admin@example.com', 'password': 'correct horse battery staple', 'totp_code': totp_code(admin_secret)})
csrf = login_payload['csrf_token']

# without the CSRF header — proves the mutation is genuinely protected, exactly what the removed 'token' field used to (incorrectly) gate
try:
    call('POST', '/api/v1/alert-rules', {'name': 'no-csrf-should-fail', 'kind': 'metric', 'metric': 'cpu', 'threshold': 90, 'severity': 'warning', 'enabled': True})
    print('UNEXPECTED: request without CSRF header succeeded')
except urllib.error.HTTPError as error:
    print('request without CSRF header correctly rejected:', error.code)

rule_payload = call('POST', '/api/v1/alert-rules', {'name': 'Yüksek CPU', 'kind': 'metric', 'metric': 'cpu', 'threshold': 90, 'severity': 'warning', 'enabled': True}, {'X-CSRF-Token': csrf})
print('alert rule created with session auth:', rule_payload['id'])
"
```

Beklenen: "request without CSRF header correctly rejected: 403" ve ardından "alert rule created with session auth: ..." satırlarını basar.

- [ ] **Adım 4: Temizlik**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
docker compose down -v
```

- [ ] **Adım 5: Go ve frontend testlerini son kez birlikte çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./... && go vet ./... && gofmt -l . && GOCACHE=/tmp/bazusop-go-cache go test ./... 2>&1 | tail -30`
Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm test 2>&1 | tail -40 && npm run build 2>&1 | tail -20`
Beklenen: hepsi BAŞARILI

## Kendi Kendine İnceleme

**1. Spec kapsaması.** Spike 13.3'ün kabul sinyali ("Giriş yapmış bir operatör/site-admin tarayıcıdan iş oluşturur, alarm kuralı/bakım penceresi ekler, olay onaylar — eski token alanı yok") → Görev 1-2'nin testleri bunu bileşen seviyesinde, Görev 4'ün Adım 3'ü gerçek bir HTTP istemcisiyle (CSRF'siz isteğin reddedildiğini de ekleyerek) kanıtlıyor.

**2. Placeholder taraması.** Görev 2 Adım 3'ün ilk taslağında ikinci test `stubBaseFetch`'i çağırıp sonucunu hemen `mockImplementation` ile tamamen eziyordu (ilk çağrı tamamen gereksizdi) ve `getByLabelText("Ad", ...)` iki eşleşen alan yüzünden belirsizlik hatası verecekti — bu, kendi kendine inceleme sırasında fark edilip tek, doğru `stubBaseFetch((url, init) => ...)` çağrısı ve `getAllByLabelText("Ad")[0]` ile düzeltildi; plan artık ilk seferde çalışan tek bir sürüm içeriyor.

**3. Tip tutarlılığı.** `apiFetch(input: string, init?: RequestInit)` imzası 13.1'de tanımlandığı gibi hem `job-panel.tsx` hem `alarm-center.tsx`'te aynı şekilde çağrılıyor (`method`, `headers`, `body` — `Authorization` header'ı hiçbir yerde artık geçmiyor). `useSession()`'ın döndürdüğü `{ apiFetch }` iki dosyada da aynı şekilde destructure ediliyor.
