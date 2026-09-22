# Frontend Oturum Kabuğu (Spike 13.1) Uygulama Planı

> **Otonom çalışanlar için:** ZORUNLU ALT-SKILL: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) veya superpowers:executing-plans kullan. Adımlar `- [ ]` checkbox söz dizimiyle takip edilir.

**Amaç:** Frontend'e gerçek bir oturum kabuğu kazandırır: `react-router-dom` ile route tabanlı gezinme, merkezi bir `SessionProvider` (whoami/login/logout/CSRF), CSRF-farkında bir `apiFetch` yardımcısı, ve oturumsuz erişimde her sayfayı `/login`'e yönlendiren bir `ProtectedRoute`. Bu spike'tan sonra tarayıcıdan gerçek bir kullanıcı adıyla giriş yapılabilir; dashboard artık kimliksiz açılmaz.

**Mimari:** Mevcut `app.tsx`'in gövdesi `app-shell.tsx`'e taşınır (yalnız veri akışı ve iç mantık korunur — navigasyon `window.history.pushState` yerine `react-router-dom`'un `useNavigate`/`useLocation`'ını kullanır, nav düğmeleri `<button>` olarak kalır — bu, mevcut geniş `app.test.tsx` süitinin `getByRole("button", ...)` sorgularını kırmadan gerçek router entegrasyonu sağlar). `app.tsx` artık yalnız `<BrowserRouter><SessionProvider><Routes>...</Routes></SessionProvider></BrowserRouter>` tanımlayan bir router köküdür. Yeni `web/src/lib/session.tsx`, oturum durumunu ve CSRF token'ını React Context'te tutar; `web/src/lib/protected-route.tsx` oturumsuzken `/login`'e yönlendirir.

**Teknoloji yığını:** React 19, TypeScript, Vite, Vitest + Testing Library, `react-router-dom` (yeni bağımlılık), Go 1.26 (küçük bir backend eklentisi).

**Spec:** [docs/superpowers/specs/2026-09-22-frontend-identity-rbac-ui-design.md](../specs/2026-09-22-frontend-identity-rbac-ui-design.md) — bu plan yalnız Spike 13.1'i (Teslimat stratejisi tablosu) kapsar.

## Genel Kısıtlar

- **Nav düğmeleri `<button>` olarak kalır, `<Link>`'e çevrilmez.** Mevcut `web/src/app.test.tsx` süiti onlarca yerde `getByRole("button", { name: "Filo" })` gibi sorgular kullanıyor; bunları `<a>`/`role="link"`'e çevirmek gereksiz, büyük bir test-kırılması yaratırdı. Router entegrasyonu, düğmelerin `onClick`'inde `window.history.pushState` yerine `useNavigate()` çağırması ve `activePage`'in `useLocation()`'dan türetilmesiyle sağlanır — bu hâlâ gerçek `react-router-dom` kullanımıdır (browser history, `useLocation` reaktivitesi, gelecekteki `/invite/:token` route'u için temel).
- **Bu spike'ta değişmeyenler:** İşler (`job-panel.tsx`) ve Alarmlar (`alarm-center.tsx`) sayfalarındaki eski "yetkili token" yapıştırma alanları — bunlar bilinçli olarak Spike 13.3'e bırakılmıştır (spec'in Teslimat stratejisi tablosu). Bulut/Aktivite/Denetim izi/Ayarlar gibi salt-okunur sayfalar da `apiFetch`'e taşınmaz — GET istekleri CSRF gerektirmez ve `ProtectedRoute` zaten tüm sayfa ağacını oturumsuzken hiç render etmiyor.
- **`main.tsx` değişmez** — `BrowserRouter` `app.tsx`'in içine yerleşir, `main.tsx` hâlâ yalnız `<App />`'i render eder.
- **`navigation.ts` değişmez** — `pageFromPath` zaten `location.pathname`'den bağımsız çalışan saf bir fonksiyon; `app-shell.tsx` onu `useLocation().pathname` ile çağırmaya devam eder.

## Dosya Yapısı

- Değiştir: `internal/server/sessions.go`, `internal/server/sessions_test.go` (whoami yanıtına CSRF token eklenir).
- Oluştur: `web/src/lib/session.tsx`, `web/src/lib/session.test.tsx`, `web/src/lib/protected-route.tsx`.
- Oluştur: `web/src/pages/login.tsx`.
- Oluştur: `web/src/app-shell.tsx` (mevcut `app.tsx` gövdesinin taşınmış hali).
- Değiştir: `web/src/app.tsx` (router köküne indirgenir), `web/src/app.test.tsx`, `web/src/styles.css`, `web/package.json`.

## Görev 1: Backend — whoami yanıtına CSRF token ekle

**Dosyalar:**
- Değiştir: `internal/server/sessions.go`
- Değiştir: `internal/server/sessions_test.go`

**Arayüzler:**
- Değiştirir: `GET /api/v1/session` yanıtı artık `csrf_token` alanı içerir (frontend'in sayfa yenilemesi sonrası CSRF token'ı yeniden öğrenmesinin TEK yolu — `bazusop_csrf` çerezi `Path: "/api"` ile sınırlı olduğundan, `/` kökünde servis edilen SPA sayfası onu `document.cookie` ile göremez; bu yüzden token JSON gövdesinde dönülür).

- [x] **Adım 1: `internal/server/sessions.go`'daki `handleWhoAmI`'ı güncelle**

```go
func handleWhoAmI(sessionService *sessions.Service, identityService *identity.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		session, _, ok := authenticateSession(response, request, sessionService)
		if !ok {
			return
		}
		user, err := identityService.UserByID(request.Context(), session.UserID)
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			UserID    string `json:"user_id"`
			Email     string `json:"email"`
			Role      string `json:"role"`
			CSRFToken string `json:"csrf_token"`
			ExpiresAt string `json:"expires_at"`
		}{user.ID, user.Email, string(user.Role), session.CSRFToken, session.AbsoluteExpiresAt.Format(timeLayout)})
	}
}
```

(Yalnız `CSRFToken string \`json:"csrf_token"\`` alanı ve `session.CSRFToken` değeri eklendi; fonksiyonun geri kalanı değişmedi.)

- [x] **Adım 2: Build'i doğrula**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./internal/server/... 2>&1 && echo BUILD_OK`
Beklenen: `BUILD_OK`

- [x] **Adım 3: `internal/server/sessions_test.go`'daki `TestLoginWhoAmILogoutEndToEnd`'i güncelle**

`whoAmIResponse` bloğunun hemen ardına, mevcut `if whoAmIResponse.Code != http.StatusOK { ... }` kontrolünden sonra ekle:

```go
	var whoAmIPayload struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(whoAmIResponse.Body).Decode(&whoAmIPayload); err != nil || whoAmIPayload.CSRFToken == "" {
		t.Fatalf("expected whoami to return a non-empty csrf_token, got %#v, %v", whoAmIPayload, err)
	}
	if whoAmIPayload.CSRFToken != loginPayload.CSRFToken {
		t.Fatalf("expected whoami's csrf_token to match the one issued at login, got %q vs %q", whoAmIPayload.CSRFToken, loginPayload.CSRFToken)
	}
```

(`whoAmIResponse.Body`, tıpkı `loginResponse.Body` gibi, `httptest.ResponseRecorder`'ın `*bytes.Buffer`'ıdır — bir kez decode edilebilir; bu test zaten `whoAmIResponse.Code`'u kontrol ettikten sonra body'yi tekrar okumuyordu, bu yüzden yeni decode çağrısı güvenli.)

- [x] **Adım 4: Testi çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... -run 'TestLoginWhoAmILogoutEndToEnd' -v 2>&1 | tail -30`
Beklenen: BAŞARILI

- [x] **Adım 5: Tam `internal/server` paket testini çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go vet ./internal/server/... && gofmt -l internal/server/*.go && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... 2>&1 | tail -10`
Beklenen: vet/gofmt çıktısı yok; `ok`

- [x] **Adım 6: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add internal/server/sessions.go internal/server/sessions_test.go
git commit -m "feat: return the CSRF token from whoami so a page refresh doesn't lose it"
```

## Görev 2: Frontend — `SessionProvider` ve `apiFetch`

**Dosyalar:**
- Oluştur: `web/src/lib/session.tsx`
- Oluştur: `web/src/lib/session.test.tsx`

**Arayüzler:**
- Üretir: `SessionProvider` (React bileşeni), `useSession()` hook'u — `{ state: SessionState, login(credentials): Promise<LoginResult>, logout(): Promise<void>, apiFetch(input, init?): Promise<Response> }` döndürür. `SessionState` bir union: `{status:"loading"} | {status:"anonymous"} | {status:"authenticated", user:{userId,email,role}, csrfToken, expiresAt}`.

- [x] **Adım 1: `web/src/lib/session.tsx`'i yaz**

```tsx
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
```

- [x] **Adım 2: `web/src/lib/session.test.tsx`'i yaz**

```tsx
import { act, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { SessionProvider, useSession } from "./session"

function Probe() {
  const { state, login, logout, apiFetch } = useSession()
  return (
    <div>
      <span data-testid="status">{state.status}</span>
      <span data-testid="email">{state.status === "authenticated" ? state.user.email : ""}</span>
      <button onClick={() => void login({ email: "admin@example.com", password: "correct horse battery staple", totpCode: "123456" })} type="button">
        login
      </button>
      <button onClick={() => void logout()} type="button">
        logout
      </button>
      <button onClick={() => void apiFetch("/api/v1/users/invites", { method: "POST" })} type="button">
        mutate
      </button>
    </div>
  )
}

describe("SessionProvider", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("starts loading then becomes anonymous when whoami fails", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false } as Response))
    render(<SessionProvider><Probe /></SessionProvider>)
    expect(screen.getByTestId("status")).toHaveTextContent("loading")
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"))
  })

  it("becomes authenticated when whoami succeeds", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ user_id: "u-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-abc", expires_at: "2026-09-22T22:00:00Z" }),
    } as Response))
    render(<SessionProvider><Probe /></SessionProvider>)
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"))
    expect(screen.getByTestId("email")).toHaveTextContent("admin@example.com")
  })

  it("login transitions anonymous to authenticated on success", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/v1/session") return Promise.resolve({ ok: false } as Response)
      if (url === "/api/v1/sessions") return Promise.resolve({
        ok: true,
        status: 201,
        json: async () => ({ user_id: "u-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-abc", expires_at: "2026-09-22T22:00:00Z" }),
      } as Response)
      return Promise.resolve({ ok: true, json: async () => ({}) } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<SessionProvider><Probe /></SessionProvider>)
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"))
    await act(async () => screen.getByRole("button", { name: "login" }).click())
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"))
  })

  it("login surfaces a wrong-credentials error without changing status", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/v1/session") return Promise.resolve({ ok: false } as Response)
      if (url === "/api/v1/sessions") return Promise.resolve({ ok: false, status: 401 } as Response)
      return Promise.resolve({ ok: true, json: async () => ({}) } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<SessionProvider><Probe /></SessionProvider>)
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"))
    await act(async () => screen.getByRole("button", { name: "login" }).click())
    expect(screen.getByTestId("status")).toHaveTextContent("anonymous")
  })

  it("apiFetch attaches the CSRF header only for non-GET requests once authenticated", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/v1/session") return Promise.resolve({
        ok: true,
        json: async () => ({ user_id: "u-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-abc", expires_at: "2026-09-22T22:00:00Z" }),
      } as Response)
      return Promise.resolve({ ok: true, status: 201, json: async () => ({}) } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<SessionProvider><Probe /></SessionProvider>)
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"))
    await act(async () => screen.getByRole("button", { name: "mutate" }).click())
    const mutateCall = fetchMock.mock.calls.find((call) => call[0] === "/api/v1/users/invites")
    expect(mutateCall).toBeDefined()
    const headers = new Headers((mutateCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-abc")
  })

  it("logout clears the session", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "/api/v1/session") return Promise.resolve({
        ok: true,
        json: async () => ({ user_id: "u-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-abc", expires_at: "2026-09-22T22:00:00Z" }),
      } as Response)
      if (url === "/api/v1/sessions") return Promise.resolve({ ok: true, status: 204 } as Response)
      return Promise.resolve({ ok: true, json: async () => ({}) } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(<SessionProvider><Probe /></SessionProvider>)
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"))
    await act(async () => screen.getByRole("button", { name: "logout" }).click())
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"))
  })
})
```

**Yazarken düzeltme:** `beforeEach`/`afterEach` içeren diğer test dosyalarının aksine, bu dosya `beforeEach` KULLANMIYOR — her test kendi `vi.stubGlobal("fetch", ...)` çağrısını yapıyor (farklı senaryolar farklı mock'lar gerektiriyor, ortak bir varsayılan gereksiz). Yalnız `afterEach(() => vi.unstubAllGlobals())` yeterli.

- [x] **Adım 3: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm test -- session.test.tsx 2>&1 | tail -60`
Beklenen: 6 test BAŞARILI

- [x] **Adım 4: Tip kontrolü**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npx tsc -b --noEmit 2>&1 | tail -40`
Beklenen: hata yok (henüz kullanılmayan bir dosya olsa bile mevcut kodla tip çakışması olmamalı)

- [x] **Adım 5: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/lib/session.tsx web/src/lib/session.test.tsx
git commit -m "feat: add SessionProvider and CSRF-aware apiFetch to the frontend"
```

## Görev 3: `react-router-dom`, `ProtectedRoute`, `LoginPage`, router köküne geçiş

**Dosyalar:**
- Değiştir: `web/package.json` (ve `npm install`'ın ürettiği `package-lock.json`)
- Oluştur: `web/src/lib/protected-route.tsx`
- Oluştur: `web/src/pages/login.tsx`
- Oluştur: `web/src/app-shell.tsx`
- Değiştir: `web/src/app.tsx`, `web/src/styles.css`

**Arayüzler:**
- Tüketir: `useSession` (`./lib/session`).
- Üretir: `ProtectedRoute` (children alan bir sarmalayıcı bileşen), `LoginPage`, `AppShell` (eski `App`'in yerini alan, artık `react-router-dom` hook'larını kullanan bileşen), yeni `App` (yalnız router tanımı).

- [x] **Adım 1: `react-router-dom`'u kur**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm install react-router-dom@^7.18.4 2>&1 | tail -20`
Beklenen: `package.json`'a `"react-router-dom": "^7.18.4"` eklenir, `package-lock.json` güncellenir, hata yok.

- [x] **Adım 2: `web/src/lib/protected-route.tsx`'i yaz**

```tsx
import type { ReactNode } from "react"
import { Navigate } from "react-router-dom"

import { useSession } from "./session"

export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { state } = useSession()
  if (state.status === "loading") {
    return (
      <div className="auth-loading" role="status">
        Yükleniyor…
      </div>
    )
  }
  if (state.status === "anonymous") return <Navigate replace to="/login" />
  return <>{children}</>
}
```

- [x] **Adım 3: `web/src/pages/login.tsx`'i yaz**

```tsx
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
```

- [x] **Adım 4: `web/src/app-shell.tsx`'i, mevcut `web/src/app.tsx`'in gövdesini taşıyıp uyarlayarak yaz**

```tsx
import { Bell, LogOut, Search } from "lucide-react"
import { useEffect, useState } from "react"
import { useLocation, useNavigate } from "react-router-dom"

import { ThemeToggle } from "./components/theme-toggle"
import { Badge } from "./components/ui/badge"
import { InstancePage } from "./components/instance-page"
import { useSession } from "./lib/session"
import { navigation, pageFromPath, pageMeta, secondaryNavigation } from "./navigation"
import { ActivityPage } from "./pages/activity"
import { AlarmCenter } from "./pages/alarm-center"
import { AuditTrailPage } from "./pages/audit"
import { CloudInventoryPage } from "./pages/cloud-inventory"
import { FleetPage } from "./pages/fleet"
import { JobPanel } from "./pages/job-panel"
import { LogPanel } from "./pages/log-panel"
import { OverviewPage } from "./pages/overview"
import { ServicesPanel } from "./pages/services-panel"
import { SettingsPage } from "./pages/settings"
import { TelemetryPanel } from "./pages/telemetry-panel"
import type { AlertIncident, InventoryInstance, LogEntry, ManagedService, OperationJob, PageID, TelemetryPayload } from "./types"

export function AppShell() {
  const location = useLocation()
  const routerNavigate = useNavigate()
  const { state: session, logout } = useSession()
  const activePage = pageFromPath(location.pathname)
  const [instances, setInstances] = useState<InventoryInstance[]>([])
  const [inventoryState, setInventoryState] = useState<"loading" | "ready" | "error">("loading")
  const [selectedInstance, setSelectedInstance] = useState<InventoryInstance | null>(null)
  const [telemetry, setTelemetry] = useState<TelemetryPayload | null>(null)
  const [telemetryState, setTelemetryState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [services, setServices] = useState<ManagedService[]>([])
  const [serviceState, setServiceState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [logState, setLogState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [jobs, setJobs] = useState<OperationJob[]>([])
  const [jobState, setJobState] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [incidents, setIncidents] = useState<AlertIncident[]>([])
  const [alertState, setAlertState] = useState<"loading" | "ready" | "error">("loading")

  useEffect(() => {
    const controller = new AbortController()
    fetch("/api/v1/instances", { signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error("inventory unavailable")
        return response.json() as Promise<{ instances: InventoryInstance[] }>
      })
      .then((payload) => {
        setInstances(payload.instances)
        setInventoryState("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setInventoryState("error")
      })
    fetch("/api/v1/incidents?limit=100", { signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error("incidents unavailable")
        return response.json() as Promise<{ incidents: AlertIncident[] }>
      })
      .then((payload) => {
        setIncidents(payload.incidents ?? [])
        setAlertState("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setAlertState("error")
      })
    return () => controller.abort()
  }, [])

  const activeIncidents = incidents.filter((incident) => incident.status !== "resolved")

  function navigate(page: PageID) {
    const item = [...navigation, ...secondaryNavigation].find((candidate) => candidate.id === page)
    if (!item) return
    routerNavigate(item.path)
    window.scrollTo({ top: 0, behavior: "smooth" })
  }

  function handleLogout() {
    logout().then(() => routerNavigate("/login", { replace: true }))
  }

  function openTelemetry(instance: InventoryInstance) {
    setSelectedInstance(instance)
    setTelemetry(null)
    setTelemetryState("loading")
    setServices([])
    setServiceState("loading")
    setLogs([])
    setLogState("loading")
    setJobs([])
    setJobState("loading")
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/telemetry?limit=288`)
      .then((response) => {
        if (!response.ok) throw new Error("telemetry unavailable")
        return response.json() as Promise<TelemetryPayload>
      })
      .then((payload) => {
        setTelemetry(payload)
        setTelemetryState("ready")
      })
      .catch(() => setTelemetryState("error"))
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/services`)
      .then((response) => {
        if (!response.ok) throw new Error("service inventory unavailable")
        return response.json() as Promise<{ services: ManagedService[] }>
      })
      .then((payload) => {
        setServices(payload.services)
        setServiceState("ready")
      })
      .catch(() => setServiceState("error"))
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/logs?limit=200`)
      .then((response) => {
        if (!response.ok) throw new Error("logs unavailable")
        return response.json() as Promise<{ entries: LogEntry[] }>
      })
      .then((payload) => {
        setLogs(payload.entries)
        setLogState("ready")
      })
      .catch(() => setLogState("error"))
    fetch(`/api/v1/instances/${encodeURIComponent(instance.agent_id)}/jobs?limit=50`)
      .then((response) => {
        if (!response.ok) throw new Error("jobs unavailable")
        return response.json() as Promise<{ jobs: OperationJob[] }>
      })
      .then((payload) => {
        setJobs(payload.jobs)
        setJobState("ready")
      })
      .catch(() => setJobState("error"))
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <div className="app-frame">
        <aside className="sidebar">
          <div className="brand">
            <div className="brand-mark" aria-hidden="true">U</div>
            <div><strong>bazUSOP</strong><span>kontrol düzlemi</span></div>
          </div>

          <nav aria-label="Ana navigasyon" className="nav-list">
            {navigation.map(({ id, icon: Icon, label }) => {
              const count = label === "Alarmlar" ? activeIncidents.length : undefined
              return (
              <button aria-current={activePage === id ? "page" : undefined} className={activePage === id ? "nav-item active" : "nav-item"} key={label} onClick={() => navigate(id)} type="button">
                <Icon aria-hidden="true" size={18} strokeWidth={1.8} />
                <span>{label}</span>
                {count !== undefined ? <span className="nav-count">{count}</span> : null}
              </button>
              )
            })}
          </nav>

          <div className="sidebar-bottom">
            {secondaryNavigation.map(({ id, icon: Icon, label }) => <button aria-current={activePage === id ? "page" : undefined} className={activePage === id ? "nav-item active" : "nav-item"} key={id} onClick={() => navigate(id)} type="button"><Icon size={18} /><span>{label}</span></button>)}
            <div className="hub-health"><span className="status-dot" />Hub çalışıyor <span>v0.1</span></div>
          </div>
        </aside>

        <main>
          <header className="topbar">
            <button className="search" type="button">
              <Search size={17} />
              <span>Sunucu, servis ve log ara…</span>
              <kbd><Command size={12} /> K</kbd>
            </button>
            <div className="topbar-actions">
              <Badge className="environment"><span className="status-dot" />Üretim</Badge>
              <ThemeToggle />
              <button aria-label="Bildirimler" className="icon-button" type="button"><Bell size={18} /></button>
              {session.status === "authenticated" ? (
                <div className="user-menu">
                  <span className="avatar" title={session.user.email}>{session.user.email.slice(0, 2).toUpperCase()}</span>
                  <div className="user-menu-details">
                    <strong>{session.user.email}</strong>
                    <span>{session.user.role}</span>
                  </div>
                  <button aria-label="Çıkış yap" className="icon-button" onClick={handleLogout} type="button"><LogOut size={16} /></button>
                </div>
              ) : null}
            </div>
          </header>

          <div className="content">
            <div className="page-heading">
              <div><p className="eyebrow">{pageMeta[activePage].eyebrow}</p><h1>{pageMeta[activePage].title}</h1><p className="page-description">{pageMeta[activePage].description}</p></div>
              <div className="live-status"><span className="pulse" />Hub bağlantısı aktif</div>
            </div>

            {activePage === "overview" ? <OverviewPage activeIncidents={activeIncidents} alertState={alertState} instances={instances} inventoryState={inventoryState} onOpenAlarmCenter={() => navigate("alerts")} /> : null}

            {activePage === "alerts" ? <AlarmCenter incidents={incidents} onIncidentUpdated={(updated) => setIncidents((current) => current.map((incident) => incident.id === updated.id ? updated : incident))} /> : null}

            {activePage === "fleet" ? <FleetPage instances={instances} inventoryState={inventoryState} onOpenMetrics={(instance) => { openTelemetry(instance); navigate("metrics") }} /> : null}

            {activePage === "metrics" ? <InstancePage instances={instances} onSelect={openTelemetry} selected={selectedInstance}>{selectedInstance ?
                <TelemetryPanel
                  instance={selectedInstance}
                  onClose={() => setSelectedInstance(null)}
                  payload={telemetry}
                  state={telemetryState}
                /> : null}</InstancePage> : null}
            {activePage === "services" ? <InstancePage instances={instances} onSelect={openTelemetry} selected={selectedInstance}>{selectedInstance ? <ServicesPanel key={selectedInstance.agent_id} instance={selectedInstance} services={services} state={serviceState} /> : null}</InstancePage> : null}
            {activePage === "logs" ? <InstancePage instances={instances} onSelect={openTelemetry} selected={selectedInstance}>{selectedInstance ? <LogPanel key={`${selectedInstance.agent_id}-logs`} entries={logs} instance={selectedInstance} state={logState} /> : null}</InstancePage> : null}
            {activePage === "jobs" ? <InstancePage instances={instances} onSelect={openTelemetry} selected={selectedInstance}>{selectedInstance ? <JobPanel
                  instance={selectedInstance}
                  jobs={jobs}
                  key={`${selectedInstance.agent_id}-jobs`}
                  onCreated={(job) => setJobs((current) => [job, ...current])}
                  state={jobState}
                /> : null}</InstancePage> : null}
            {activePage === "cloud" ? <CloudInventoryPage /> : null}
            {activePage === "activity" ? <ActivityPage /> : null}
            {activePage === "audit" ? <AuditTrailPage /> : null}
            {activePage === "settings" ? <SettingsPage /> : null}
          </div>
        </main>
      </div>
    </div>
  )
}
```

**Yazarken düzeltme:** Orijinal `app.tsx`'te `Command` ikonu `lucide-react`'tan import ediliyordu (`import { Bell, Command, Search } from "lucide-react"`); yukarıdaki JSX onu hâlâ kullanıyor (`<kbd><Command size={12} /> K</kbd>`) ama import satırına `LogOut` eklenirken `Command` yanlışlıkla düşürülmüş görünüyor — import satırını `import { Bell, Command, LogOut, Search } from "lucide-react"` olarak düzelt.

- [x] **Adım 5: `web/src/app.tsx`'i router köküne indirge**

```tsx
import { BrowserRouter, Route, Routes } from "react-router-dom"

import { AppShell } from "./app-shell"
import { ProtectedRoute } from "./lib/protected-route"
import { SessionProvider } from "./lib/session"
import { LoginPage } from "./pages/login"

export function App() {
  return (
    <BrowserRouter>
      <SessionProvider>
        <Routes>
          <Route element={<LoginPage />} path="/login" />
          <Route
            element={
              <ProtectedRoute>
                <AppShell />
              </ProtectedRoute>
            }
            path="/*"
          />
        </Routes>
      </SessionProvider>
    </BrowserRouter>
  )
}
```

- [x] **Adım 6: `web/src/styles.css`'e giriş ekranı ve kullanıcı menüsü stillerini ekle**

Dosyanın sonuna ekle:

```css
.auth-shell { min-height: 100vh; display: grid; place-items: center; background: var(--bg); padding: 24px; }
.auth-card { width: 100%; max-width: 360px; padding: 28px; border: 1px solid var(--border); border-radius: 16px; background: var(--surface); display: grid; gap: 14px; }
.auth-card h1 { margin: 0; color: var(--strong); font-size: 20px; font-weight: 650; }
.auth-card label { display: grid; gap: 6px; color: var(--subtle); font-size: 12px; }
.auth-card input { min-height: 40px; padding: 0 12px; border: 1px solid var(--border); border-radius: 8px; outline: 0; color: var(--strong); background: var(--card); font: inherit; }
.auth-card input:focus { border-color: var(--accent); }
.auth-card button[type="submit"] { min-height: 42px; border: 0; border-radius: 8px; background: var(--accent); color: var(--strong); font-weight: 650; cursor: pointer; }
.auth-card button[type="submit"]:disabled { opacity: .6; cursor: default; }
.auth-toggle { border: 0; background: none; color: var(--accent); font-size: 12px; text-align: left; cursor: pointer; padding: 0; }
.auth-error { margin: 0; color: var(--danger, #e5484d); font-size: 12px; }
.auth-loading { min-height: 100vh; display: grid; place-items: center; color: var(--subtle); }
.user-menu { display: flex; align-items: center; gap: 8px; padding-left: 8px; border-left: 1px solid var(--border); margin-left: 4px; }
.user-menu-details { display: none; }
.user-menu-details strong { display: block; color: var(--strong); font-size: 12px; }
.user-menu-details span { display: block; color: var(--subtle); font-size: 11px; text-transform: capitalize; }
@media (min-width: 1100px) { .user-menu-details { display: block; } }
```

- [x] **Adım 7: Tip kontrolü ve production build**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm run build 2>&1 | tail -60`
Beklenen: BAŞARILI (bu adımda `app.test.tsx` henüz güncellenmediği için testler BAŞARISIZ olabilir — Görev 4 düzeltir; `npm run build` yalnız tip kontrolü + prod bundle'ı doğrular, testleri çalıştırmaz)

- [x] **Adım 8: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/package.json web/package-lock.json web/src/lib/protected-route.tsx web/src/pages/login.tsx web/src/app-shell.tsx web/src/app.tsx web/src/styles.css
git commit -m "feat: add react-router-dom, ProtectedRoute and a real login page"
```

## Görev 4: `app.test.tsx`'i oturum-farkında hale getir

**Dosyalar:**
- Değiştir: `web/src/app.test.tsx`

**Arayüzler:**
- Tüketir: `App` (`./app`), `LoginPage` (`./pages/login`), `SessionProvider` (`./lib/session`).

- [x] **Adım 1: `web/src/app.test.tsx`'in tamamını yeniden yaz**

Mevcut dosyadaki her senaryo korunur; tek fark, `beforeEach`'in varsayılan `fetch` mock'unun artık `GET /api/v1/session`'ı da (oturumlu) yanıtlaması, ve her özel `mockImplementation`'ın da aynı `/api/v1/session` dalını eklemesi. Yeni üç test (login akışı, oturumsuz yönlendirme, çıkış) sona eklenir.

```tsx
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { App } from "./app"
import { LandingPage } from "./landing-page"

const authenticatedWhoAmI = { user_id: "user-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-token-abc", expires_at: "2026-09-22T22:00:00Z" }

function respondWithSession(url: string) {
  return url.endsWith("/api/v1/session") ? Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response) : null
}

describe("bazUSOP shell", () => {
  beforeEach(() => {
    window.localStorage.clear()
    window.history.replaceState({}, "", "/")
    delete document.documentElement.dataset.theme
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      return respondWithSession(url) ?? Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    }))
    vi.stubGlobal("scrollTo", vi.fn())
  })

  afterEach(() => vi.unstubAllGlobals())

  it("presents the unified operations overview", async () => {
    render(<App />)

    expect(await screen.findByRole("heading", { name: "Operasyon özeti" })).toBeInTheDocument()
    expect(screen.getByText("Sunucular")).toBeInTheDocument()
    expect(screen.getByText("Ortalama CPU")).toBeInTheDocument()
    expect(screen.getByText("Açık alarmlar")).toBeInTheDocument()
    expect(screen.getByLabelText("Filo özeti").children).toHaveLength(3)
    expect(screen.getByRole("region", { name: "Operasyon alarmları" })).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Son 24 saatte filo kaynak kullanımı" })).toBeInTheDocument()
    expect(screen.queryByRole("table", { name: "Sunucu sağlığı" })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Filo" }))
    expect(screen.getByRole("heading", { name: "Sunucu filosu" })).toBeInTheDocument()
    expect(screen.getByRole("table", { name: "Sunucu sağlığı" })).toBeInTheDocument()
  })

  it("presents a verifiable public product story", () => {
    render(<LandingPage />)

    expect(screen.getByRole("heading", { name: "Sunucu operasyonları için tek çalışma yüzeyi." })).toBeInTheDocument()
    expect(screen.getByRole("navigation", { name: "Landing page navigasyonu" })).toBeInTheDocument()
    expect(screen.getByRole("region", { name: "Canlı operasyon görünümü" })).toBeInTheDocument()
    expect(screen.getByRole("list", { name: "Platform kabiliyetleri" }).children).toHaveLength(4)
    expect(screen.getByRole("heading", { name: "Sinyalden müdahaleye, bağlam kaybetmeden." })).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Kimlikten başlayan güvenlik." })).toBeInTheDocument()
    expect(screen.getAllByRole("link", { name: /GitHub'da incele/ })[0]).toHaveAttribute(
      "href",
      "https://github.com/gokayybaz/bazusop",
    )
  })

  it("opens application pages directly from their URL", async () => {
    window.history.replaceState({}, "", "/settings")
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const sessionResponse = respondWithSession(url)
      if (sessionResponse) return sessionResponse
      if (url.endsWith("/api/v1/system/configuration")) return Promise.resolve({ ok: true, json: async () => ({ storage: "postgresql", timescale_enabled: true, telemetry_retention_days: 30, log_retention_days: 14, version: "0.3.0", commit: "abc123def456", build_date: "2026-09-12T09:30:00Z" }) } as Response)
      return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    })

    render(<App />)

    expect(await screen.findByRole("heading", { name: "Ayarlar" })).toBeInTheDocument()
    expect(screen.getByText("Görünüm")).toBeInTheDocument()
    expect(await screen.findByText("Telemetri: 30 gün")).toBeInTheDocument()
    expect(screen.getByText("Loglar: 14 gün")).toBeInTheDocument()
    expect(screen.getByText("0.3.0")).toBeInTheDocument()
    expect(screen.getByText("abc123def456")).toBeInTheDocument()
    expect(screen.queryByRole("region", { name: "Operasyon alarmları" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Ayarlar" })).toHaveAttribute("aria-current", "page")
  })

  it("shows managed alarm incidents and opens the alarm center", async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const sessionResponse = respondWithSession(url)
      if (sessionResponse) return sessionResponse
      if (url.includes("/incidents")) return Promise.resolve({ ok: true, json: async () => ({ incidents: [{ id: "incident-01", rule_id: "rule-01", rule_name: "Disk kritik eşiği", agent_id: "db-01", severity: "critical", status: "open", message: "Disk 96.0%; eşik 90.0%", latest_value: 96, opened_at: "2026-09-11T08:00:00Z" }] }) } as Response)
      return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    })
    render(<App />)
    expect(await screen.findByText("Disk kritik eşiği")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Alarmlar 1" })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Alarmlar 1" }))
    expect(window.location.pathname).toBe("/alerts")
    expect(screen.getByRole("region", { name: "Alarm merkezi" })).toBeInTheDocument()
    expect(screen.getByText("Politika değişiklikleri yönetici, olay onayı operatör yetkisi ister.")).toBeInTheDocument()
    expect(screen.getByLabelText("Yetkili token")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "incident-01 olayını onayla" })).toBeInTheDocument()
  })

  it("lets the operator switch between light and dark themes", async () => {
    render(<App />)

    fireEvent.click((await screen.findAllByRole("button", { name: "Açık temayı kullan" }))[0])

    expect(document.documentElement.dataset.theme).toBe("light")
    expect(document.documentElement.style.colorScheme).toBe("light")
    expect(window.localStorage.getItem("bazusop-theme")).toBe("light")
    expect(screen.getAllByRole("button", { name: "Koyu temayı kullan" }).length).toBeGreaterThan(0)
  })

  it("shows provider accounts and safely reconciled cloud instances on their own page", async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const sessionResponse = respondWithSession(url)
      if (sessionResponse) return sessionResponse
      if (url.endsWith("/api/v1/cloud/accounts")) return Promise.resolve({ ok: true, json: async () => ({ accounts: [{ id: "account-01", name: "Üretim AWS", provider: "aws", external_id: "123456789012", status: "connected", last_sync_at: "2026-09-11T18:00:00Z" }] }) } as Response)
      if (url.endsWith("/api/v1/cloud/instances")) return Promise.resolve({ ok: true, json: async () => ({ instances: [{ account_id: "account-01", account_name: "Üretim AWS", provider: "aws", provider_instance_id: "i-0123", name: "edge-01", region: "eu-central-1", state: "running", os_family: "linux", private_ips: ["10.0.0.8"], public_ips: [], agent_id: "agent-01", match_status: "verified", match_reason: "provider_agent_id" }] }) } as Response)
      if (url.includes("/incidents")) return Promise.resolve({ ok: true, json: async () => ({ incidents: [] }) } as Response)
      return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    })

    render(<App />)
    fireEvent.click(await screen.findByRole("button", { name: "Bulut hesapları" }))

    expect(window.location.pathname).toBe("/cloud")
    expect(await screen.findAllByText("Üretim AWS")).toHaveLength(2)
    expect(screen.getByRole("table", { name: "Bulut sunucuları" })).toBeInTheDocument()
    expect(screen.getByText("i-0123")).toBeInTheDocument()
    expect(screen.getByText("Doğrulandı")).toBeInTheDocument()
    expect(screen.getByText("agent-01")).toBeInTheDocument()
  })

  it("shows a unified, time-ordered activity timeline merging job and alert events", async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const sessionResponse = respondWithSession(url)
      if (sessionResponse) return sessionResponse
      if (url.startsWith("/api/v1/activity/events")) return Promise.resolve({ ok: true, json: async () => ({ events: [
        { organization_id: "org_default", site_id: "site_default", source: "alert", reference_id: "incident-01", agent_id: "agent-01", type: "opened", actor: "hub", message: "CPU %96", occurred_at: "2026-09-11T08:05:00Z" },
        { organization_id: "org_default", site_id: "site_default", source: "job", reference_id: "job-01", agent_id: "agent-01", type: "approved", actor: "gokay", message: "config rollout", occurred_at: "2026-09-11T08:00:00Z" },
      ] }) } as Response)
      if (url.includes("/incidents")) return Promise.resolve({ ok: true, json: async () => ({ incidents: [] }) } as Response)
      return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    })

    render(<App />)
    fireEvent.click(await screen.findByRole("button", { name: "Aktivite" }))

    expect(window.location.pathname).toBe("/activity")
    expect(await screen.findByRole("table", { name: "Aktivite olayları" })).toBeInTheDocument()
    expect(screen.getByText("Onaylandı")).toBeInTheDocument()
    expect(screen.getByText("Açıldı")).toBeInTheDocument()
    expect(screen.getByText("config rollout")).toBeInTheDocument()
    expect(screen.getByText("CPU %96")).toBeInTheDocument()
    const rows = screen.getAllByRole("row")
    expect(rows[1]).toHaveTextContent("Açıldı")
    expect(rows[2]).toHaveTextContent("Onaylandı")
  })

  it("shows a permission-filtered audit trail search with working filters", async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const sessionResponse = respondWithSession(url)
      if (sessionResponse) return sessionResponse
      if (url.startsWith("/api/v1/audit/events")) return Promise.resolve({ ok: true, json: async () => ({ events: [
        { event_id: "e-1", occurred_at: "2026-09-11T08:05:00Z", correlation_id: "c-1", actor_type: "human", actor_id: "user-1", session_or_token_id: "", organization_id: "org_default", site_id: "site_default", action: "POST", permission: "manage_service_accounts", resource_type: "service_accounts", resource_id: "account-1", outcome: "success", error_code: "", source_ip: "", user_agent: "", change_summary: "" },
      ] }) } as Response)
      if (url.includes("/incidents")) return Promise.resolve({ ok: true, json: async () => ({ incidents: [] }) } as Response)
      return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    })

    render(<App />)
    fireEvent.click(await screen.findByRole("button", { name: "Denetim izi" }))

    expect(window.location.pathname).toBe("/audit")
    const table = await screen.findByRole("table", { name: "Denetim kayıtları" })
    expect(table).toBeInTheDocument()
    expect(within(table).getByText("account-1")).toBeInTheDocument()
    expect(within(table).getByText("Başarılı")).toBeInTheDocument()
  })

  it("renders enrolled instances returned by the inventory API", async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const sessionResponse = respondWithSession(url)
      if (sessionResponse) return sessionResponse
      if (url.endsWith("/api/v1/instances")) return Promise.resolve({
        ok: true,
        json: async () => ({
          instances: [{
            agent_id: "agent-01",
            hostname: "edge-01.example.com",
            os_family: "linux",
            os_name: "Ubuntu",
            os_version: "24.04",
            architecture: "amd64",
            kernel_version: "6.8.0",
            cpu_cores: 8,
            memory_bytes: 17179869184,
            ip_addresses: ["10.0.0.8"],
            agent_version: "0.2.0",
            first_seen_at: "2026-09-10T19:00:00Z",
            last_seen_at: "2026-09-10T20:00:00Z",
            status: "connected",
          }],
        }),
      } as Response)
      if (url.includes("/incidents")) return Promise.resolve({ ok: true, json: async () => ({ incidents: [] }) } as Response)
      if (url.includes("/telemetry")) return Promise.resolve({
        ok: true,
        json: async () => ({
          latest: { recorded_at: "2026-09-11T04:05:00Z", cpu_percent: 47.8, memory_percent: 63.4, disk_percent: 71.1, network_rx_bytes: 2048, network_tx_bytes: 1024 },
          samples: [
            { recorded_at: "2026-09-11T04:00:00Z", cpu_percent: 42.5, memory_percent: 62.1, disk_percent: 71, network_rx_bytes: 1024, network_tx_bytes: 512 },
            { recorded_at: "2026-09-11T04:05:00Z", cpu_percent: 47.8, memory_percent: 63.4, disk_percent: 71.1, network_rx_bytes: 2048, network_tx_bytes: 1024 },
          ],
        }),
      } as Response)
      if (url.includes("/services")) return Promise.resolve({
        ok: true,
        json: async () => ({
          services: [
            { agent_id: "agent-01", name: "nginx.service", display_name: "NGINX Web Server", state: "running", startup_type: "automatic", observed_at: "2026-09-11T04:05:00Z" },
            { agent_id: "agent-01", name: "queue-worker.service", display_name: "Queue Worker", state: "failed", startup_type: "automatic", observed_at: "2026-09-11T04:05:00Z" },
          ],
        }),
      } as Response)
      if (url.includes("/logs")) return Promise.resolve({
        ok: true,
        json: async () => ({
          entries: [
            { id: "log-01", agent_id: "agent-01", occurred_at: "2026-09-11T04:04:00Z", collector: "journald", source: "nginx.service", severity: "info", message: "worker process started" },
            { id: "log-02", agent_id: "agent-01", occurred_at: "2026-09-11T04:05:00Z", collector: "journald", source: "nginx.service", severity: "error", message: "upstream timeout" },
          ],
        }),
      } as Response)
      if (url.includes("/jobs")) return Promise.resolve({
        ok: true,
        json: async () => ({
          jobs: [{ id: "job-01", agent_id: "agent-01", action: "service.restart", target: "nginx.service", approved_by: "gokay", reason: "Yapılandırmayı etkinleştir", requested_at: "2026-09-11T04:06:00Z", status: "succeeded", last_sequence: 3, signature: "signed", signing_public_key: "key" }],
        }),
      } as Response)
      return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    })

    render(<App />)

    fireEvent.click(await screen.findByRole("button", { name: "Filo" }))
    expect(await screen.findByText("edge-01.example.com")).toBeInTheDocument()
    expect(screen.getByText("Ubuntu 24.04")).toBeInTheDocument()
    expect(screen.getByText("8 çekirdek · 16 GiB")).toBeInTheDocument()
    expect(screen.getByText("10.0.0.8")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "edge-01.example.com metriklerini aç" }))

    expect(window.location.pathname).toBe("/metrics")
    expect(await screen.findByRole("region", { name: "edge-01.example.com telemetrisi" })).toBeInTheDocument()
    expect(screen.getByText("47.8%")).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Son 24 saat CPU ve bellek kullanımı" })).toBeInTheDocument()
    expect(screen.queryByRole("region", { name: "edge-01.example.com servisleri" })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Servisler" }))
    expect(await screen.findByRole("region", { name: "edge-01.example.com servisleri" })).toBeInTheDocument()
    expect(screen.getByText("NGINX Web Server")).toBeInTheDocument()
    expect(screen.getByText("Queue Worker")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Başarısız servisleri göster" }))
    expect(screen.queryByText("NGINX Web Server")).not.toBeInTheDocument()
    expect(screen.getByText("Queue Worker")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Loglar" }))
    expect(await screen.findByRole("region", { name: "edge-01.example.com logları" })).toBeInTheDocument()
    expect(screen.getByText("worker process started")).toBeInTheDocument()
    expect(screen.getByText("upstream timeout")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Canlı akışı başlat" })).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Hata loglarını göster" }))
    expect(screen.queryByText("worker process started")).not.toBeInTheDocument()
    expect(screen.getByText("upstream timeout")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "İşler" }))
    expect(await screen.findByRole("region", { name: "edge-01.example.com işleri" })).toBeInTheDocument()
    expect(screen.getByText("Servisi yeniden başlat")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "job-01 işinin audit kaydını aç" }))
    expect(await screen.findByText("nginx.service yeniden başlatıldı")).toBeInTheDocument()
  })

  it("redirects to the login page when there is no session", async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: false, status: 401 } as Response)
      return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    })

    render(<App />)

    expect(await screen.findByRole("heading", { name: "Giriş yap" })).toBeInTheDocument()
    expect(window.location.pathname).toBe("/login")
    expect(screen.queryByRole("heading", { name: "Operasyon özeti" })).not.toBeInTheDocument()
  })

  it("logs in successfully and lands on the dashboard", async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: false, status: 401 } as Response)
      if (url.endsWith("/api/v1/sessions")) return Promise.resolve({ ok: true, status: 201, json: async () => authenticatedWhoAmI } as Response)
      return Promise.resolve({ ok: true, json: async () => ({ instances: [] }) } as Response)
    })
    window.history.replaceState({}, "", "/login")

    render(<App />)

    expect(await screen.findByRole("heading", { name: "Giriş yap" })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("E-posta"), { target: { value: "admin@example.com" } })
    fireEvent.change(screen.getByLabelText("Parola"), { target: { value: "correct horse battery staple" } })
    fireEvent.change(screen.getByLabelText("Doğrulayıcı kodu"), { target: { value: "123456" } })
    fireEvent.click(screen.getByRole("button", { name: "Giriş yap" }))

    expect(await screen.findByRole("heading", { name: "Operasyon özeti" })).toBeInTheDocument()
    expect(window.location.pathname).toBe("/")
    expect(screen.getByText("admin@example.com")).toBeInTheDocument()
  })

  it("logs out and returns to the login page", async () => {
    render(<App />)

    fireEvent.click(await screen.findByRole("button", { name: "Çıkış yap" }))

    await waitFor(() => expect(screen.getByRole("heading", { name: "Giriş yap" })).toBeInTheDocument())
    expect(window.location.pathname).toBe("/login")
  })
})
```

- [x] **Adım 2: Testleri çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm test 2>&1 | tail -150`
Beklenen: TÜM testler BAŞARILI

- [x] **Adım 3: Production build'i tekrar doğrula**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm run build 2>&1 | tail -40`
Beklenen: BAŞARILI

- [x] **Adım 4: Commit**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
git add web/src/app.test.tsx
git commit -m "test: make the frontend test suite session-aware and cover login/logout"
```

## Görev 5: Docker Compose ile manuel doğrulama

**Dosyalar:** yok (yalnız doğrulama).

- [x] **Adım 1: Docker Compose ile hub'ı yeniden derleyip ayağa kaldır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && docker compose down -v >/dev/null 2>&1; BAZUSOP_PORT=8090 BAZUSOP_BOOTSTRAP_SECRET=verify-bootstrap BAZUSOP_TOTP_ENCRYPTION_KEY=verify-totp-key BAZUSOP_SERVICE_ACCOUNT_PEPPER=verify-pepper docker compose up --build -d 2>&1 | tail -30`

- [x] **Adım 2: Sağlık kontrolünü bekle**

Çalıştır: `for i in $(seq 1 20); do curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8090/api/v1/health | grep -q 200 && echo healthy && break; sleep 1; done`

- [x] **Adım 3: Bootstrap ol, tarayıcı benzeri bir akışla giriş yap (curl ile, gerçek tarayıcı davranışını simüle ederek)**

```bash
rm -f /tmp/bazusop-1310-cookies.txt
curl -s -c /tmp/bazusop-1310-cookies.txt -X POST http://127.0.0.1:8090/api/v1/bootstrap \
  -d '{"secret":"verify-bootstrap","email":"admin@example.com","password":"correct horse battery staple"}' > /tmp/bazusop-1310-bootstrap.json
cat /tmp/bazusop-1310-bootstrap.json
```

- [x] **Adım 4: Ana sayfanın (oturumsuz) giriş ekranına yönlendiren bir SPA index döndürdüğünü doğrula**

```bash
curl -s http://127.0.0.1:8090/ | grep -o '<title>[^<]*</title>'
```

Beklenen: `<title>bazUSOP · Operasyonlar</title>` (SPA kabuğu döner; giriş yönlendirmesi client-side router tarafından yapılır — bu adım yalnız statik dosyanın servis edildiğini doğrular, gerçek yönlendirme tarayıcıda çalışır).

- [x] **Adım 5: Whoami'nin artık csrf_token döndürdüğünü doğrula**

```bash
CODE=$(python3 -c "import json; print(json.load(open('/tmp/bazusop-1310-bootstrap.json'))['recovery_codes'][0])")
curl -s -c /tmp/bazusop-1310-cookies.txt -b /tmp/bazusop-1310-cookies.txt -X POST http://127.0.0.1:8090/api/v1/sessions \
  -d '{"email":"admin@example.com","password":"correct horse battery staple","recovery_code":"'"$CODE"'"}' -o /tmp/bazusop-1310-login.json
curl -s -b /tmp/bazusop-1310-cookies.txt http://127.0.0.1:8090/api/v1/session -w "\nstatus:%{http_code}\n"
```

Beklenen: yanıt gövdesinde `csrf_token` alanı dolu ve `/tmp/bazusop-1310-login.json`'daki login yanıtının `csrf_token`'ıyla aynı.

- [x] **Adım 6: Gerçek tarayıcıda manuel doğrulama (bilgi amaçlı)**

`http://127.0.0.1:8090` tarayıcıda açıldığında: (a) oturum yoksa `/login` ekranına yönlenmeli, (b) `admin@example.com` / `correct horse battery staple` / az önceki recovery code ile giriş yapılabilmeli, (c) dashboard'da sağ üstte gerçek e-posta ve "Çıkış yap" görünmeli, (d) çıkış yapınca `/login`'e dönmeli. Bu adım otomatik değildir; önceki adımların API seviyesinde kanıtladığı akışın tarayıcıda da tutarlı olduğunu teyit eder.

- [x] **Adım 7: Temizlik**

```bash
cd /Users/gokaybaz/Documents/ChatGPT/bazusop
docker compose down -v
rm -f /tmp/bazusop-1310-cookies.txt /tmp/bazusop-1310-bootstrap.json /tmp/bazusop-1310-login.json
```

- [x] **Adım 8: Go ve frontend testlerini son kez birlikte çalıştır**

Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop && go build ./... && go vet ./... && GOCACHE=/tmp/bazusop-go-cache go test ./internal/server/... 2>&1 | tail -10`
Çalıştır: `cd /Users/gokaybaz/Documents/ChatGPT/bazusop/web && npm test 2>&1 | tail -30 && npm run build 2>&1 | tail -20`
Beklenen: hepsi BAŞARILI

## Kendi Kendine İnceleme

**1. Spec kapsaması.** Spike 13.1'in tablo satırı ("Router + oturum kabuğu... Kabul sinyali: Oturumsuz erişimde her sayfa /login'e yönlenir; doğru kimlik bilgisiyle giriş dashboard'u açar; topbar gerçek kullanıcıyı gösterir") → Görev 2-4 (SessionProvider/ProtectedRoute/LoginPage + app-shell.tsx'teki gerçek kullanıcı gösterimi) + Görev 5'in Adım 6'sı ile tarayıcıda doğrulanıyor. "Whoami'nin CSRF token döndürmesi" mimari gereksinimi Görev 1'de karşılanıyor.

**2. Placeholder taraması.** Her adımda gerçek, eksiksiz kod var; "Yazarken düzeltme" notları (whoami body decode sırası, `Command` ikonunun import'ta korunması) ilk taslaktaki gerçek hataları düzeltiyor.

**3. Tip tutarlılığı.** `SessionState`/`SessionUser`/`LoginCredentials`/`LoginResult` Görev 2'de tanımlandığı gibi Görev 3 (`ProtectedRoute`, `LoginPage`) ve Görev 4'te (test mock'larının `WhoAmIPayload` alan adları: `user_id`, `email`, `role`, `csrf_token`, `expires_at`) tutarlı kullanılıyor. `apiFetch` imzası (`input: string, init？: RequestInit`) Görev 2'de tanımlanıp bu spike'ta başka hiçbir dosyada henüz tüketilmiyor (13.3'e bırakıldı, Genel Kısıtlar'da açıkça belirtildi) — bu kasıtlı, "no placeholder" ihlali değil çünkü `apiFetch`'in kendisi tam ve test edilmiş durumda.
