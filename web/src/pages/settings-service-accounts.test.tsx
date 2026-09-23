import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SettingsServiceAccountsTab } from "./settings-service-accounts"
import { SessionProvider } from "../lib/session"
import type { ServiceAccount } from "../types"

const authenticatedWhoAmI = { user_id: "admin-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-token-abc", expires_at: "2026-09-23T22:00:00Z" }

const baseAccounts: ServiceAccount[] = [
  { id: "acct-1", name: "ci-bot", role: "operator", created_at: "2026-09-20T10:00:00Z", active_token: { id: "token-1", expires_at: "2026-12-19T10:00:00Z" } },
  { id: "acct-2", name: "backup-bot", role: "viewer", created_at: "2026-09-21T10:00:00Z", disabled_at: "2026-09-22T10:00:00Z", active_token: null },
]

const listURL = "/api/v1/sites/site_default/service-accounts"

describe("SettingsServiceAccountsTab", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("shows the service account table with token and status badges", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) return Promise.resolve({ ok: true, json: async () => ({ service_accounts: baseAccounts }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )

    const table = await screen.findByRole("table", { name: "Servis hesapları" })
    expect(within(table).getByText("ci-bot")).toBeInTheDocument()
    expect(within(table).getByText("backup-bot")).toBeInTheDocument()
    expect(within(table).getByText("Aktif token var")).toBeInTheDocument()
    expect(within(table).getByText("Token yok")).toBeInTheDocument()
    expect(within(table).getByText("Aktif")).toBeInTheDocument()
    expect(within(table).getByText("Devre dışı")).toBeInTheDocument()
  })

  it("shows a forbidden state for a non-site-admin session", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => ({ ...authenticatedWhoAmI, role: "operator" }) } as Response)
      if (url.endsWith(listURL)) return Promise.resolve({ ok: false, status: 403 } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )

    expect(await screen.findByRole("heading", { name: "Servis hesabı yönetimine erişim yetkiniz yok" })).toBeInTheDocument()
  })

  it("creates a service account and reveals the one-time token with a CSRF header", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [] }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )
    await screen.findByText("Henüz servis hesabı yok.")

    fetchMock.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL) && (!init || init.method === undefined)) return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [] }) } as Response)
      if (url.endsWith(listURL) && init?.method === "POST") return Promise.resolve({ ok: true, status: 201, json: async () => ({ id: "acct-3", name: "ci-bot", role: "operator", token: "bazusop_sat_newid_newsecret" }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    })

    fireEvent.change(screen.getByLabelText("Ad"), { target: { value: "ci-bot" } })
    fireEvent.click(screen.getByRole("button", { name: "Servis hesabı oluştur" }))

    expect(await screen.findByText("bazusop_sat_newid_newsecret")).toBeInTheDocument()
    const createCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith(listURL) && (call[1] as RequestInit)?.method === "POST")
    expect(createCall).toBeDefined()
    const headers = new Headers((createCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })

  it("rotates a token and reveals the new one-time token with a CSRF header", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [baseAccounts[0]] }) } as Response)
      if (url.endsWith("/acct-1/rotate") && init?.method === "POST") return Promise.resolve({ ok: true, status: 201, json: async () => ({ token: "bazusop_sat_rotatedid_rotatedsecret" }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )
    await screen.findByRole("table", { name: "Servis hesapları" })

    fireEvent.click(screen.getByRole("button", { name: "Token yenile" }))

    expect(await screen.findByText("bazusop_sat_rotatedid_rotatedsecret")).toBeInTheDocument()
    const rotateCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/acct-1/rotate"))
    expect(rotateCall).toBeDefined()
    const headers = new Headers((rotateCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })

  it("revokes the active token with a CSRF header and disables the revoke button once none remains", async () => {
    let listCallCount = 0
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) {
        listCallCount += 1
        const account = listCallCount === 1 ? baseAccounts[0] : { ...baseAccounts[0], active_token: null }
        return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [account] }) } as Response)
      }
      if (url.endsWith("/service-accounts/tokens/token-1") && init?.method === "DELETE") return Promise.resolve({ ok: true, status: 204 } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )
    await screen.findByRole("table", { name: "Servis hesapları" })

    fireEvent.click(screen.getByRole("button", { name: "Token iptal et" }))

    expect(await screen.findByText("ci-bot token'ı iptal edildi.")).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole("button", { name: "Token iptal et" })).toBeDisabled())
    const revokeCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/service-accounts/tokens/token-1"))
    expect(revokeCall).toBeDefined()
    const headers = new Headers((revokeCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })

  it("disables a service account with a CSRF header and shows the disabled status", async () => {
    let listCallCount = 0
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith(listURL)) {
        listCallCount += 1
        const account = listCallCount === 1 ? baseAccounts[0] : { ...baseAccounts[0], disabled_at: "2026-09-23T12:00:00Z", active_token: null }
        return Promise.resolve({ ok: true, json: async () => ({ service_accounts: [account] }) } as Response)
      }
      if (url.endsWith("/service-accounts/acct-1") && init?.method === "DELETE") return Promise.resolve({ ok: true, status: 204 } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsServiceAccountsTab />
      </SessionProvider>,
    )
    const table = await screen.findByRole("table", { name: "Servis hesapları" })
    expect(within(table).getByText("Aktif")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Devre dışı bırak" }))

    expect(await screen.findByText("ci-bot devre dışı bırakıldı.")).toBeInTheDocument()
    await waitFor(() => expect(within(table).getByText("Devre dışı")).toBeInTheDocument())
    const disableCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/service-accounts/acct-1") && (call[1] as RequestInit)?.method === "DELETE")
    expect(disableCall).toBeDefined()
    const headers = new Headers((disableCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })
})
