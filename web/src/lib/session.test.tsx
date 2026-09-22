import { act, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

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
