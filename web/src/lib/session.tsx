import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react"

export type SessionUser = { userId: string; email: string; role: string }

export type SessionState =
  | { status: "loading" }
  | { status: "anonymous" }
  | { status: "authenticated"; user: SessionUser; csrfToken: string; expiresAt: string }

export type LoginCredentials = { email: string; password: string; totpCode?: string; recoveryCode?: string }
export type LoginResult = { ok: true } | { ok: false; message: string }

type WhoAmIPayload = { user_id: string; email: string; role: string; csrf_token: string; expires_at: string }

type SessionContextValue = {
  state: SessionState
  login: (credentials: LoginCredentials) => Promise<LoginResult>
  logout: () => Promise<void>
  apiFetch: (input: string, init?: RequestInit) => Promise<Response>
}

const SessionContext = createContext<SessionContextValue | null>(null)

function toUser(payload: WhoAmIPayload): SessionState {
  return { status: "authenticated", user: { userId: payload.user_id, email: payload.email, role: payload.role }, csrfToken: payload.csrf_token, expiresAt: payload.expires_at }
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<SessionState>({ status: "loading" })
  const stateRef = useRef(state)
  stateRef.current = state

  useEffect(() => {
    let cancelled = false
    fetch("/api/v1/session")
      .then((response) => {
        if (!response.ok) throw new Error("anonymous")
        return response.json() as Promise<WhoAmIPayload>
      })
      .then((payload) => {
        if (!cancelled) setState(toUser(payload))
      })
      .catch(() => {
        if (!cancelled) setState({ status: "anonymous" })
      })
    return () => {
      cancelled = true
    }
  }, [])

  const apiFetch = useCallback(async (input: string, init: RequestInit = {}) => {
    const method = (init.method ?? "GET").toUpperCase()
    const headers = new Headers(init.headers)
    const current = stateRef.current
    if (method !== "GET" && method !== "HEAD" && current.status === "authenticated") {
      headers.set("X-CSRF-Token", current.csrfToken)
    }
    const response = await fetch(input, { ...init, headers })
    if (response.status === 401) setState({ status: "anonymous" })
    return response
  }, [])

  const login = useCallback(async (credentials: LoginCredentials): Promise<LoginResult> => {
    let response: Response
    try {
      response = await fetch("/api/v1/sessions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email: credentials.email,
          password: credentials.password,
          totp_code: credentials.totpCode ?? "",
          recovery_code: credentials.recoveryCode ?? "",
        }),
      })
    } catch {
      return { ok: false, message: "Hub'a ulaşılamıyor, bağlantınızı kontrol edin." }
    }
    if (response.status === 429) return { ok: false, message: "Çok fazla deneme yapıldı, birkaç dakika sonra tekrar deneyin." }
    if (!response.ok) return { ok: false, message: "E-posta, parola veya kod hatalı." }
    const payload = (await response.json()) as WhoAmIPayload
    setState(toUser(payload))
    return { ok: true }
  }, [])

  const logout = useCallback(async () => {
    const current = stateRef.current
    if (current.status !== "authenticated") return
    await fetch("/api/v1/sessions", { method: "DELETE", headers: { "X-CSRF-Token": current.csrfToken } })
    setState({ status: "anonymous" })
  }, [])

  return <SessionContext.Provider value={{ state, login, logout, apiFetch }}>{children}</SessionContext.Provider>
}

export function useSession(): SessionContextValue {
  const context = useContext(SessionContext)
  if (!context) throw new Error("useSession must be used within a SessionProvider")
  return context
}
