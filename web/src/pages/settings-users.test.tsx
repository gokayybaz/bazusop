import { fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SettingsUsersTab } from "./settings-users"
import { SessionProvider } from "../lib/session"
import type { ManagedUser } from "../types"

const authenticatedWhoAmI = { user_id: "admin-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-token-abc", expires_at: "2026-09-23T22:00:00Z" }

const baseUsers: ManagedUser[] = [
  { id: "admin-1", email: "admin@example.com", role: "platform-admin", totp_confirmed_at: "2026-09-01T00:00:00Z", site_roles: [] },
  { id: "user-2", email: "operator@example.com", role: "", totp_confirmed_at: "2026-09-02T00:00:00Z", site_roles: [{ site_id: "site_default", role: "operator" }] },
]

describe("SettingsUsersTab", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("shows the user table with site roles and status badges", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/api/v1/users")) return Promise.resolve({ ok: true, json: async () => ({ users: baseUsers }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsUsersTab />
      </SessionProvider>,
    )

    const table = await screen.findByRole("table", { name: "Kullanıcılar" })
    expect(within(table).getByText("admin@example.com")).toBeInTheDocument()
    expect(within(table).getByText("operator@example.com")).toBeInTheDocument()
    expect(within(table).getByText("site_default: operator")).toBeInTheDocument()
    expect(within(table).getAllByText("Onaylı")).toHaveLength(2)
    expect(within(table).getAllByText("Aktif")).toHaveLength(2)
  })

  it("shows a forbidden state for a non-admin session", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => ({ ...authenticatedWhoAmI, role: "operator" }) } as Response)
      if (url.endsWith("/api/v1/users")) return Promise.resolve({ ok: false, status: 403 } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsUsersTab />
      </SessionProvider>,
    )

    expect(await screen.findByRole("heading", { name: "Kullanıcı yönetimine erişim yetkiniz yok" })).toBeInTheDocument()
  })

  it("creates an invite and shows the resulting link with a CSRF header", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/api/v1/users")) return Promise.resolve({ ok: true, json: async () => ({ users: baseUsers }) } as Response)
      if (url.endsWith("/api/v1/users/invites")) return Promise.resolve({ ok: true, status: 201, json: async () => ({ email: "new@example.com", token: "invite-token-abc" }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsUsersTab />
      </SessionProvider>,
    )
    await screen.findByRole("table", { name: "Kullanıcılar" })

    fireEvent.change(screen.getByLabelText("E-posta"), { target: { value: "new@example.com" } })
    fireEvent.click(screen.getByRole("button", { name: "Davet oluştur" }))

    expect(await screen.findByText("invite-token-abc", { exact: false })).toBeInTheDocument()
    const inviteCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/api/v1/users/invites"))
    expect(inviteCall).toBeDefined()
    const headers = new Headers((inviteCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })

  it("assigns a site role with a CSRF header and refreshes the list", async () => {
    const updatedUsers: ManagedUser[] = [
      baseUsers[0],
      { ...baseUsers[1], site_roles: [{ site_id: "site_default", role: "operator" }, { site_id: "site-b", role: "viewer" }] },
    ]
    let usersCallCount = 0
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/api/v1/users")) {
        usersCallCount += 1
        return Promise.resolve({ ok: true, json: async () => ({ users: usersCallCount === 1 ? baseUsers : updatedUsers }) } as Response)
      }
      if (url.endsWith("/memberships")) return Promise.resolve({ ok: true, status: 204 } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <SettingsUsersTab />
      </SessionProvider>,
    )
    await screen.findByRole("table", { name: "Kullanıcılar" })

    fireEvent.change(screen.getByLabelText("Kullanıcı"), { target: { value: "user-2" } })
    fireEvent.change(screen.getByLabelText("Site ID"), { target: { value: "site-b" } })
    fireEvent.click(screen.getByRole("button", { name: "Rolü ata" }))

    expect(await screen.findByText("site-b: viewer")).toBeInTheDocument()
    const assignCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/api/v1/sites/site-b/memberships") && (call[1] as RequestInit)?.method === "POST")
    expect(assignCall).toBeDefined()
    const headers = new Headers((assignCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })
})
