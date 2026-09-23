import { type FormEvent, useEffect, useState } from "react"
import { KeyRound } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { useSession } from "../lib/session"
import type { ServiceAccount } from "../types"

const jsonHeaders = { "Content-Type": "application/json" }
const siteID = "site_default"

export function SettingsServiceAccountsTab() {
  const { apiFetch } = useSession()
  const [accounts, setAccounts] = useState<ServiceAccount[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error" | "forbidden">("loading")
  const [name, setName] = useState("")
  const [role, setRole] = useState<"site-admin" | "operator" | "viewer">("operator")
  const [expiryDays, setExpiryDays] = useState("")
  const [createError, setCreateError] = useState("")
  const [revealedToken, setRevealedToken] = useState<{ name: string; token: string } | null>(null)
  const [actionMessage, setActionMessage] = useState("")

  function loadAccounts() {
    setState("loading")
    fetch(`/api/v1/sites/${siteID}/service-accounts`)
      .then((response) => {
        if (response.status === 401 || response.status === 403) return Promise.reject(new Error("forbidden"))
        if (!response.ok) return Promise.reject(new Error("unavailable"))
        return response.json() as Promise<{ service_accounts: ServiceAccount[] }>
      })
      .then((payload) => {
        setAccounts(payload.service_accounts ?? [])
        setState("ready")
      })
      .catch((error: unknown) => setState((error as Error).message === "forbidden" ? "forbidden" : "error"))
  }

  useEffect(() => {
    loadAccounts()
  }, [])

  function createAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateError("")
    setRevealedToken(null)
    apiFetch(`/api/v1/sites/${siteID}/service-accounts`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ name, role, expiry_days: expiryDays ? Number(expiryDays) : 0 }),
    })
      .then((response) => {
        if (!response.ok) throw new Error()
        return response.json() as Promise<{ id: string; name: string; role: string; token: string }>
      })
      .then((payload) => {
        setRevealedToken({ name: payload.name, token: payload.token })
        setName("")
        setExpiryDays("")
        loadAccounts()
      })
      .catch(() => setCreateError("Servis hesabı oluşturulamadı; alanları kontrol edin."))
  }

  function rotateToken(account: ServiceAccount) {
    setActionMessage("")
    setRevealedToken(null)
    apiFetch(`/api/v1/service-accounts/${encodeURIComponent(account.id)}/rotate`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ expiry_days: 0 }),
    })
      .then((response) => {
        if (!response.ok) throw new Error()
        return response.json() as Promise<{ token: string }>
      })
      .then((payload) => {
        setRevealedToken({ name: account.name, token: payload.token })
        loadAccounts()
      })
      .catch(() => setActionMessage(`${account.name} için token yenilenemedi.`))
  }

  function revokeToken(account: ServiceAccount) {
    if (!account.active_token) return
    setActionMessage("")
    apiFetch(`/api/v1/service-accounts/tokens/${encodeURIComponent(account.active_token.id)}`, { method: "DELETE" })
      .then((response) => {
        if (!response.ok) throw new Error()
        setActionMessage(`${account.name} token'ı iptal edildi.`)
        loadAccounts()
      })
      .catch(() => setActionMessage(`${account.name} token'ı iptal edilemedi.`))
  }

  function disableAccount(account: ServiceAccount) {
    setActionMessage("")
    apiFetch(`/api/v1/service-accounts/${encodeURIComponent(account.id)}`, { method: "DELETE" })
      .then((response) => {
        if (!response.ok) throw new Error()
        setActionMessage(`${account.name} devre dışı bırakıldı.`)
        loadAccounts()
      })
      .catch(() => setActionMessage(`${account.name} devre dışı bırakılamadı.`))
  }

  if (state === "forbidden") {
    return <EmptyFeature icon={KeyRound} text="Bu ekranı görüntülemek için site yöneticisi oturumu gerekir." title="Servis hesabı yönetimine erişim yetkiniz yok" />
  }
  if (state === "error") {
    return <EmptyFeature icon={KeyRound} text="Hub bağlantısını kontrol edin." title="Servis hesaplarına ulaşılamıyor" />
  }

  return (
    <div className="settings-users">
      {revealedToken ? (
        <Card className="settings-form-card token-reveal-card">
          <h3>{revealedToken.name} için token</h3>
          <p className="auth-warning">Bu token yalnız bir kez gösterilir. Güvenli bir yere kaydedin.</p>
          <code>{revealedToken.token}</code>
          <button onClick={() => void navigator.clipboard?.writeText(revealedToken.token)} type="button">
            Kopyala
          </button>
        </Card>
      ) : null}

      <Card className="table-card page-card">
        <div className="card-header">
          <div>
            <h2>Servis hesapları</h2>
            <p>Otomasyon ve entegrasyonlar için kullanılan servis hesapları</p>
          </div>
        </div>
        {state === "loading" ? (
          <p className="muted">Yükleniyor…</p>
        ) : accounts.length === 0 ? (
          <p className="muted">Henüz servis hesabı yok.</p>
        ) : (
          <div className="table-scroll">
            <table aria-label="Servis hesapları">
              <thead>
                <tr>
                  <th>Ad</th>
                  <th>Rol</th>
                  <th>Oluşturulma</th>
                  <th>Token durumu</th>
                  <th>Durum</th>
                  <th>Aksiyonlar</th>
                </tr>
              </thead>
              <tbody>
                {accounts.map((account) => (
                  <tr key={account.id}>
                    <td>{account.name}</td>
                    <td>{account.role}</td>
                    <td>{account.created_at}</td>
                    <td>
                      <Badge className={account.active_token ? "totp-status confirmed" : "totp-status pending"}>
                        {account.active_token ? "Aktif token var" : "Token yok"}
                      </Badge>
                    </td>
                    <td>
                      <Badge className={account.disabled_at ? "user-status disabled" : "user-status active"}>
                        {account.disabled_at ? "Devre dışı" : "Aktif"}
                      </Badge>
                    </td>
                    <td>
                      <div className="service-account-actions">
                        <button disabled={Boolean(account.disabled_at)} onClick={() => rotateToken(account)} type="button">
                          Token yenile
                        </button>
                        <button disabled={!account.active_token} onClick={() => revokeToken(account)} type="button">
                          Token iptal et
                        </button>
                        <button disabled={Boolean(account.disabled_at)} onClick={() => disableAccount(account)} type="button">
                          Devre dışı bırak
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {actionMessage ? <p aria-live="polite">{actionMessage}</p> : null}
      </Card>

      <div className="settings-users-forms">
        <Card className="settings-form-card">
          <h3>Yeni servis hesabı oluştur</h3>
          <form className="alarm-form" onSubmit={createAccount}>
            <label>
              <span>Ad</span>
              <input onChange={(event) => setName(event.target.value)} required value={name} />
            </label>
            <label>
              <span>Rol</span>
              <select onChange={(event) => setRole(event.target.value as typeof role)} value={role}>
                <option value="site-admin">Site yöneticisi</option>
                <option value="operator">Operatör</option>
                <option value="viewer">İzleyici</option>
              </select>
            </label>
            <label>
              <span>Geçerlilik (gün, opsiyonel)</span>
              <input max={365} min={1} onChange={(event) => setExpiryDays(event.target.value)} placeholder="90" type="number" value={expiryDays} />
            </label>
            <button type="submit">Servis hesabı oluştur</button>
            {createError ? (
              <p aria-live="polite" className="auth-error">
                {createError}
              </p>
            ) : null}
          </form>
        </Card>
      </div>
    </div>
  )
}
