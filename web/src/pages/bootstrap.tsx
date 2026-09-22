import { type FormEvent, useState } from "react"
import { Link, useNavigate } from "react-router-dom"

import { RecoveryCodes } from "../components/recovery-codes"
import { TotpQrCode } from "../components/totp-qr-code"

type BootstrapResult = { provisioningUri: string; recoveryCodes: string[] }

export function BootstrapPage() {
  const navigate = useNavigate()
  const [secret, setSecret] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState("")
  const [alreadyBootstrapped, setAlreadyBootstrapped] = useState(false)
  const [result, setResult] = useState<BootstrapResult | null>(null)

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setError("")
    setAlreadyBootstrapped(false)
    fetch("/api/v1/bootstrap", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ secret, email, password }),
    })
      .then(async (response) => {
        if (response.status === 409) {
          setAlreadyBootstrapped(true)
          return
        }
        if (!response.ok) {
          setError("Kurulum secret'ı, e-posta veya parola hatalı.")
          return
        }
        const payload = (await response.json()) as { provisioning_uri: string; recovery_codes: string[] }
        setResult({ provisioningUri: payload.provisioning_uri, recoveryCodes: payload.recovery_codes })
      })
      .catch(() => setError("Hub'a ulaşılamıyor, bağlantınızı kontrol edin."))
      .finally(() => setSubmitting(false))
  }

  if (alreadyBootstrapped) {
    return (
      <div className="auth-shell">
        <div className="auth-card">
          <h1>Hub zaten kurulu</h1>
          <p>Bu hub daha önce kuruldu. Devam etmek için giriş yapın.</p>
          <Link className="auth-toggle" to="/login">
            Girişe git
          </Link>
        </div>
      </div>
    )
  }

  if (result) {
    return (
      <div className="auth-shell">
        <div className="auth-card auth-card-wide">
          <h1>Kurulum tamamlandı</h1>
          <p>Doğrulayıcı uygulamanızla aşağıdaki QR kodu okutun.</p>
          <TotpQrCode provisioningUri={result.provisioningUri} />
          <RecoveryCodes codes={result.recoveryCodes} />
          <button onClick={() => navigate("/login")} type="button">
            Girişe devam et
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="auth-shell">
      <form aria-label="İlk kurulum" className="auth-card" onSubmit={handleSubmit}>
        <div className="brand">
          <div aria-hidden="true" className="brand-mark">U</div>
          <div>
            <strong>bazUSOP</strong>
            <span>kontrol düzlemi</span>
          </div>
        </div>
        <h1>İlk kurulum</h1>
        <label>
          <span>Kurulum secret'ı</span>
          <input autoComplete="off" onChange={(event) => setSecret(event.target.value)} required type="password" value={secret} />
        </label>
        <label>
          <span>Yönetici e-postası</span>
          <input autoComplete="username" onChange={(event) => setEmail(event.target.value)} required type="email" value={email} />
        </label>
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
          {submitting ? "Kuruluyor…" : "Kurulumu tamamla"}
        </button>
        <Link className="auth-setup-link" to="/login">
          Zaten kuruluysa giriş yap
        </Link>
      </form>
    </div>
  )
}
