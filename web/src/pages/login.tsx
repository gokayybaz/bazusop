import { type FormEvent, useState } from "react"
import { Navigate, useNavigate } from "react-router-dom"

import { useSession } from "../lib/session"

export function LoginPage() {
  const { state, login } = useSession()
  const navigate = useNavigate()
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [useRecoveryCode, setUseRecoveryCode] = useState(false)
  const [totpCode, setTotpCode] = useState("")
  const [recoveryCode, setRecoveryCode] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState("")

  if (state.status === "authenticated") return <Navigate replace to="/" />

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSubmitting(true)
    setError("")
    login({
      email,
      password,
      totpCode: useRecoveryCode ? undefined : totpCode,
      recoveryCode: useRecoveryCode ? recoveryCode : undefined,
    })
      .then((result) => {
        if (!result.ok) {
          setError(result.message)
          return
        }
        navigate("/", { replace: true })
      })
      .finally(() => setSubmitting(false))
  }

  return (
    <div className="auth-shell">
      <form aria-label="Giriş" className="auth-card" onSubmit={handleSubmit}>
        <div className="brand">
          <div aria-hidden="true" className="brand-mark">U</div>
          <div>
            <strong>bazUSOP</strong>
            <span>kontrol düzlemi</span>
          </div>
        </div>
        <h1>Giriş yap</h1>
        <label>
          <span>E-posta</span>
          <input autoComplete="username" onChange={(event) => setEmail(event.target.value)} required type="email" value={email} />
        </label>
        <label>
          <span>Parola</span>
          <input autoComplete="current-password" onChange={(event) => setPassword(event.target.value)} required type="password" value={password} />
        </label>
        {useRecoveryCode ? (
          <label>
            <span>Kurtarma kodu</span>
            <input autoComplete="one-time-code" onChange={(event) => setRecoveryCode(event.target.value)} required value={recoveryCode} />
          </label>
        ) : (
          <label>
            <span>Doğrulayıcı kodu</span>
            <input autoComplete="one-time-code" inputMode="numeric" maxLength={6} onChange={(event) => setTotpCode(event.target.value)} required value={totpCode} />
          </label>
        )}
        <button className="auth-toggle" onClick={() => setUseRecoveryCode((current) => !current)} type="button">
          {useRecoveryCode ? "Doğrulayıcı kodu kullan" : "Kurtarma kodu kullan"}
        </button>
        {error ? (
          <p aria-live="polite" className="auth-error">
            {error}
          </p>
        ) : null}
        <button disabled={submitting} type="submit">
          {submitting ? "Giriş yapılıyor…" : "Giriş yap"}
        </button>
      </form>
    </div>
  )
}
