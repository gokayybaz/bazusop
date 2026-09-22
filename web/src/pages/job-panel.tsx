import { type FormEvent, useState } from "react"
import { ChevronRight, ListChecks } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { formatLastSeen } from "../lib/format"
import { useSession } from "../lib/session"
import type { InventoryInstance, JobEvent, OperationJob } from "../types"

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

  function openAudit(job: OperationJob) {
    setSelectedJob(job)
    setEvents([])
    setEventState("loading")
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/jobs/${encodeURIComponent(job.id)}/events`)
      .then((response) => {
        if (!response.ok) throw new Error("audit unavailable")
        return response.json() as Promise<{ events: JobEvent[] }>
      })
      .then((payload) => {
        setEvents(payload.events)
        setEventState("ready")
      })
      .catch(() => setEventState("error"))
  }

  return (
    <Card aria-label={`${instance.hostname} işleri`} className="jobs-card">
      <div className="card-header jobs-header">
        <div><h2>Operasyon işleri</h2><p>{instance.hostname} · imzalı ve denetlenebilir aksiyonlar</p></div>
        <button className="text-button job-create-toggle" onClick={() => setCreating((current) => !current)} type="button">{creating ? "İptal" : "Yeni iş oluştur"}</button>
      </div>
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
      {state === "loading" ? <div className="service-state">İş geçmişi yükleniyor…</div> : null}
      {state === "error" ? <div className="service-state">İş geçmişine şu anda ulaşılamıyor.</div> : null}
      {state === "ready" && jobs.length === 0 ? <div className="service-state">Bu sunucu için henüz operasyon işi yok.</div> : null}
      {jobs.length > 0 ? (
        <div className="job-list">
          {jobs.map((job) => (
            <button aria-label={`${job.id} işinin audit kaydını aç`} className={selectedJob?.id === job.id ? "job-row active" : "job-row"} key={job.id} onClick={() => openAudit(job)} type="button">
              <span className="job-action"><ListChecks aria-hidden="true" size={17} /><span><strong>{jobActionLabel(job.action)}</strong><small>{job.target || instance.hostname}</small></span></span>
              <span className="job-approval"><strong>{job.approved_by}</strong><small>{job.reason}</small></span>
              <Badge className={`job-status ${job.status}`}>{jobStatusLabel(job.status)}</Badge>
              <time dateTime={job.requested_at}>{formatLastSeen(job.requested_at)}</time>
              <ChevronRight aria-hidden="true" size={16} />
            </button>
          ))}
        </div>
      ) : null}
      {selectedJob ? (
        <div className="job-audit">
          <div className="job-audit-heading"><strong>Audit kaydı</strong><span>{selectedJob.id}</span></div>
          {eventState === "loading" ? <div className="job-event-state">Olaylar yükleniyor…</div> : null}
          {eventState === "error" ? <div className="job-event-state">Audit kaydına ulaşılamıyor.</div> : null}
          {events.map((event) => (
            <div className={`job-event ${event.type}`} key={event.sequence}>
              <span className="job-event-sequence">#{event.sequence}</span>
              <span><strong>{jobEventLabel(event.type)}</strong><small>{event.actor} · {formatLastSeen(event.occurred_at)}</small></span>
              <code>{event.message}</code>
            </div>
          ))}
        </div>
      ) : null}
    </Card>
  )
}

function jobActionLabel(action: OperationJob["action"]) {
  return action === "service.restart" ? "Servisi yeniden başlat" : "Sunucuyu yeniden başlat"
}

function jobStatusLabel(status: OperationJob["status"]) {
  return { queued: "Sırada", running: "Çalışıyor", succeeded: "Başarılı", failed: "Başarısız" }[status]
}

function jobEventLabel(type: JobEvent["type"]) {
  return { approved: "Onaylandı", claimed: "Agent teslim aldı", output: "Çıktı", succeeded: "Tamamlandı", failed: "Başarısız" }[type]
}
