import { type FormEvent, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"

import { RecoveryCodes } from "../components/recovery-codes"
import { TotpQrCode } from "../components/totp-qr-code"

type Step =
  | { name: "password" }
  | { name: "totp"; userId: string; provisioningUri: string }
  | { name: "done"; recoveryCodes: string[] }

export function InvitePage() {
  const { token = "" } = useParams<{ token: string }>()
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>({ name: "password" })
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState("")

  function handleConsume(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setError("")
    fetch(`/api/v1/invites/${encodeURIComponent(token)}/consume`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password }),
    })
      .then(async (response) => {
        if (response.status === 404) {
          setError("Bu davet bulunamadı.")
          return
        }
        if (response.status === 409) {
          setError("Bu davetin süresi dolmuş veya zaten kullanılmış.")
          return
        }
        if (response.status === 403) {
          setError("Bu davet bu giriş yöntemiyle kullanılamaz.")
          return
        }
        if (response.status === 429) {
          setError("Çok fazla deneme yapıldı, birkaç dakika sonra tekrar deneyin.")
          return
        }
        if (!response.ok) {
          setError("Davet kabul edilemedi.")
          return
        }
        const payload = (await response.json()) as { id: string; provisioning_uri: string }
        setStep({ name: "totp", userId: payload.id, provisioningUri: payload.provisioning_uri })
      })
      .catch(() => setError("Hub'a ulaşılamıyor, bağlantınızı kontrol edin."))
      .finally(() => setSubmitting(false))
  }

  function handleConfirm(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (step.name !== "totp") return
    setSubmitting(true)
    setError("")
    fetch(`/api/v1/users/${encodeURIComponent(step.userId)}/confirm-totp`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code }),
    })
      .then(async (response) => {
        if (response.status === 429) {
          setError("Çok fazla deneme yapıldı, birkaç dakika sonra tekrar deneyin.")
          return
        }
        if (!response.ok) {
          setError("Doğrulayıcı kodu hatalı.")
          return
        }
        const payload = (await response.json()) as { recovery_codes: string[] }
        setStep({ name: "done", recoveryCodes: payload.recovery_codes })
      })
      .catch(() => setError("Hub'a ulaşılamıyor, bağlantınızı kontrol edin."))
      .finally(() => setSubmitting(false))
  }

  if (step.name === "done") {
    return (
      <div className="auth-shell">
        <div className="auth-card auth-card-wide">
          <h1>Hesabınız hazır</h1>
          <RecoveryCodes codes={step.recoveryCodes} />
          <button onClick={() => navigate("/login")} type="button">
            Girişe devam et
          </button>
        </div>
      </div>
    )
  }

  if (step.name === "totp") {
    return (
      <div className="auth-shell">
        <form aria-label="Doğrulayıcı kurulumu" className="auth-card auth-card-wide" onSubmit={handleConfirm}>
          <h1>Doğrulayıcı uygulamasını bağla</h1>
          <p>Aşağıdaki QR kodu doğrulayıcı uygulamanızla okutun, sonra üretilen kodu girin.</p>
          <TotpQrCode provisioningUri={step.provisioningUri} />
          <label>
            <span>Doğrulayıcı kodu</span>
            <input autoComplete="one-time-code" inputMode="numeric" maxLength={6} onChange={(event) => setCode(event.target.value)} required value={code} />
          </label>
          {error ? (
            <p aria-live="polite" className="auth-error">
              {error}
            </p>
          ) : null}
          <button disabled={submitting} type="submit">
            {submitting ? "Doğrulanıyor…" : "Kodu doğrula"}
          </button>
        </form>
      </div>
    )
  }

  return (
    <div className="auth-shell">
      <form aria-label="Daveti kabul et" className="auth-card" onSubmit={handleConsume}>
        <div className="brand">
          <div aria-hidden="true" className="brand-mark">U</div>
          <div>
            <strong>bazUSOP</strong>
            <span>kontrol düzlemi</span>
          </div>
        </div>
        <h1>Hesabınızı oluşturun</h1>
        <label>
          <span>Parola</span>
          <input autoComplete="new-password" onChange={(event) => setPassword(event.target.value)} required type="password" value={password} />
        </label>
        {error ? (
          <p aria-live="polite" className="auth-error">
            {error}
          </p>
        ) : null}
        <button disabled={submitting} type="submit">
          {submitting ? "Gönderiliyor…" : "Devam et"}
        </button>
        <Link className="auth-setup-link" to="/login">
          Zaten hesabınız var mı? Giriş yapın
        </Link>
      </form>
    </div>
  )
}
