import { type FormEvent, useEffect, useState } from "react"
import { ShieldCheck } from "lucide-react"

import { Badge } from "../components/ui/badge"
import { Card } from "../components/ui/card"
import { EmptyFeature } from "../components/empty-feature"
import { useSession } from "../lib/session"
import type { ManagedUser } from "../types"

const jsonHeaders = { "Content-Type": "application/json" }

export function SettingsUsersTab() {
  const { apiFetch } = useSession()
  const [users, setUsers] = useState<ManagedUser[]>([])
  const [state, setState] = useState<"loading" | "ready" | "error" | "forbidden">("loading")
  const [inviteEmail, setInviteEmail] = useState("")
  const [inviteRole, setInviteRole] = useState<"" | "site-admin" | "operator" | "viewer">("")
  const [inviteSiteIds, setInviteSiteIds] = useState("site_default")
  const [inviteLink, setInviteLink] = useState("")
  const [inviteError, setInviteError] = useState("")
  const [roleUserId, setRoleUserId] = useState("")
  const [roleSiteId, setRoleSiteId] = useState("site_default")
  const [roleValue, setRoleValue] = useState<"site-admin" | "operator" | "viewer">("viewer")
  const [roleMessage, setRoleMessage] = useState("")

  function loadUsers() {
    setState("loading")
    fetch("/api/v1/users")
      .then((response) => {
        if (response.status === 401 || response.status === 403) return Promise.reject(new Error("forbidden"))
        if (!response.ok) return Promise.reject(new Error("unavailable"))
        return response.json() as Promise<{ users: ManagedUser[] }>
      })
      .then((payload) => {
        setUsers(payload.users ?? [])
        setState("ready")
      })
      .catch((error: unknown) => setState((error as Error).message === "forbidden" ? "forbidden" : "error"))
  }

  useEffect(() => {
    loadUsers()
  }, [])

  function createInvite(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setInviteError("")
    setInviteLink("")
    apiFetch("/api/v1/users/invites", {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({
        email: inviteEmail,
        role: inviteRole,
        site_ids: inviteRole ? inviteSiteIds.split(",").map((id) => id.trim()).filter(Boolean) : [],
      }),
    })
      .then((response) => {
        if (!response.ok) throw new Error()
        return response.json() as Promise<{ email: string; token: string }>
      })
      .then((payload) => {
        setInviteLink(`${window.location.origin}/invite/${payload.token}`)
        setInviteEmail("")
      })
      .catch(() => setInviteError("Davet oluşturulamadı; alanları kontrol edin."))
  }

  function assignRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setRoleMessage("")
    apiFetch(`/api/v1/sites/${encodeURIComponent(roleSiteId)}/memberships`, {
      method: "POST",
      headers: jsonHeaders,
      body: JSON.stringify({ user_id: roleUserId, role: roleValue }),
    })
      .then((response) => {
        if (!response.ok) throw new Error()
        setRoleMessage("Rol atandı.")
        loadUsers()
      })
      .catch(() => setRoleMessage("Rol atanamadı; kullanıcı ve site ID'sini kontrol edin."))
  }

  function revokeRole() {
    setRoleMessage("")
    apiFetch(`/api/v1/sites/${encodeURIComponent(roleSiteId)}/memberships/${encodeURIComponent(roleUserId)}`, { method: "DELETE" })
      .then((response) => {
        if (!response.ok) throw new Error()
        setRoleMessage("Rol kaldırıldı.")
        loadUsers()
      })
      .catch(() => setRoleMessage("Rol kaldırılamadı; kullanıcı ve site ID'sini kontrol edin."))
  }

  if (state === "forbidden") {
    return <EmptyFeature icon={ShieldCheck} text="Bu ekranı görüntülemek için platform yöneticisi oturumu gerekir." title="Kullanıcı yönetimine erişim yetkiniz yok" />
  }
  if (state === "error") {
    return <EmptyFeature icon={ShieldCheck} text="Hub bağlantısını kontrol edin." title="Kullanıcılara ulaşılamıyor" />
  }

  return (
    <div className="settings-users">
      <Card className="table-card page-card">
        <div className="card-header">
          <div>
            <h2>Kullanıcılar</h2>
            <p>Organizasyondaki tüm kullanıcılar ve site rolleri</p>
          </div>
        </div>
        {state === "loading" ? (
          <p className="muted">Yükleniyor…</p>
        ) : users.length === 0 ? (
          <p className="muted">Henüz kullanıcı yok.</p>
        ) : (
          <div className="table-scroll">
            <table aria-label="Kullanıcılar">
              <thead>
                <tr>
                  <th>E-posta</th>
                  <th>Rol</th>
                  <th>Site rolleri</th>
                  <th>TOTP</th>
                  <th>Durum</th>
                </tr>
              </thead>
              <tbody>
                {users.map((user) => (
                  <tr key={user.id}>
                    <td>{user.email}</td>
                    <td>{user.role || "—"}</td>
                    <td>
                      {user.site_roles.length === 0
                        ? "—"
                        : user.site_roles.map((entry) => (
                            <Badge className="site-role-badge" key={`${entry.site_id}:${entry.role}`}>
                              {entry.site_id}: {entry.role}
                            </Badge>
                          ))}
                    </td>
                    <td>
                      <Badge className={user.totp_confirmed_at ? "totp-status confirmed" : "totp-status pending"}>
                        {user.totp_confirmed_at ? "Onaylı" : "Onaysız"}
                      </Badge>
                    </td>
                    <td>
                      <Badge className={user.disabled_at ? "user-status disabled" : "user-status active"}>
                        {user.disabled_at ? "Devre dışı" : "Aktif"}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <div className="settings-users-forms">
        <Card className="settings-form-card">
          <h3>Kullanıcı davet et</h3>
          <form className="alarm-form" onSubmit={createInvite}>
            <label>
              <span>E-posta</span>
              <input onChange={(event) => setInviteEmail(event.target.value)} required type="email" value={inviteEmail} />
            </label>
            <label>
              <span>Rol</span>
              <select onChange={(event) => setInviteRole(event.target.value as typeof inviteRole)} value={inviteRole}>
                <option value="">Platform yöneticisi</option>
                <option value="site-admin">Site yöneticisi</option>
                <option value="operator">Operatör</option>
                <option value="viewer">İzleyici</option>
              </select>
            </label>
            {inviteRole ? (
              <label>
                <span>Site ID'leri (virgülle ayrılmış)</span>
                <input onChange={(event) => setInviteSiteIds(event.target.value)} required value={inviteSiteIds} />
              </label>
            ) : null}
            <button type="submit">Davet oluştur</button>
            {inviteError ? (
              <p aria-live="polite" className="auth-error">
                {inviteError}
              </p>
            ) : null}
            {inviteLink ? (
              <p aria-live="polite">
                Davet bağlantısı: <code>{inviteLink}</code>
              </p>
            ) : null}
          </form>
        </Card>

        <Card className="settings-form-card">
          <h3>Site rolü ata / kaldır</h3>
          <form className="alarm-form" onSubmit={assignRole}>
            <label>
              <span>Kullanıcı</span>
              <select onChange={(event) => setRoleUserId(event.target.value)} required value={roleUserId}>
                <option value="">Seçin…</option>
                {users.map((user) => (
                  <option key={user.id} value={user.id}>
                    {user.email}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>Site ID</span>
              <input onChange={(event) => setRoleSiteId(event.target.value)} required value={roleSiteId} />
            </label>
            <label>
              <span>Rol</span>
              <select onChange={(event) => setRoleValue(event.target.value as typeof roleValue)} value={roleValue}>
                <option value="site-admin">Site yöneticisi</option>
                <option value="operator">Operatör</option>
                <option value="viewer">İzleyici</option>
              </select>
            </label>
            <div className="settings-form-actions">
              <button disabled={!roleUserId || !roleSiteId} type="submit">
                Rolü ata
              </button>
              <button disabled={!roleUserId || !roleSiteId} onClick={revokeRole} type="button">
                Rolü kaldır
              </button>
            </div>
            {roleMessage ? <p aria-live="polite">{roleMessage}</p> : null}
          </form>
        </Card>
      </div>
    </div>
  )
}
