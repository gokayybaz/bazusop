import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SettingsPage } from "./settings"
import { SessionProvider } from "../lib/session"

const authenticatedWhoAmI = { user_id: "admin-1", email: "admin@example.com", role: "platform-admin", csrf_token: "csrf-token-abc", expires_at: "2026-09-23T22:00:00Z" }

describe("SettingsPage", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("defaults to the general tab and switches to the users tab on click", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/api/v1/system/configuration")) return Promise.resolve({ ok: true, json: async () => ({ storage: "memory", timescale_enabled: false, telemetry_retention_days: 30, log_retention_days: 14, version: "0.5.0", commit: "abc123", build_date: "2026-09-23T00:00:00Z" }) } as Response)
      if (url.endsWith("/api/v1/users")) return Promise.resolve({ ok: true, json: async () => ({ users: [] }) } as Response)
      return Promise.resolve({ ok: false } as Response)
    }))

    render(
      <SessionProvider>
        <SettingsPage />
      </SessionProvider>,
    )

    expect(await screen.findByText("Görünüm")).toBeInTheDocument()
    expect(screen.queryByText("Kullanıcı davet et")).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole("tab", { name: "Kullanıcılar" }))

    expect(await screen.findByText("Kullanıcı davet et")).toBeInTheDocument()
    expect(screen.queryByText("Görünüm")).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getByText("Henüz kullanıcı yok.")).toBeInTheDocument())
  })
})
