import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { AlarmCenter } from "./alarm-center"
import { SessionProvider } from "../lib/session"
import type { AlertIncident, AlertRule } from "../types"

const authenticatedWhoAmI = { user_id: "user-1", email: "operator@example.com", role: "operator", csrf_token: "csrf-token-abc", expires_at: "2026-09-22T22:00:00Z" }

const openIncident: AlertIncident = {
  id: "incident-01", rule_id: "rule-01", rule_name: "Disk kritik eşiği", agent_id: "db-01", severity: "critical",
  status: "open", message: "Disk 96.0%; eşik 90.0%", latest_value: 96, opened_at: "2026-09-11T08:00:00Z",
}

function stubBaseFetch(overrides: (url: string, init?: RequestInit) => Response | Promise<Response> | undefined) {
  return vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const overridden = overrides(url, init)
    if (overridden) return Promise.resolve(overridden)
    if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
    if (url.endsWith("/api/v1/alert-rules")) return Promise.resolve({ ok: true, json: async () => ({ rules: [] }) } as Response)
    if (url.endsWith("/api/v1/maintenance-windows")) return Promise.resolve({ ok: true, json: async () => ({ windows: [] }) } as Response)
    return Promise.resolve({ ok: false } as Response)
  })
}

describe("AlarmCenter", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("has no access-token field and acknowledges an incident with the session's CSRF header", async () => {
    const acknowledged: AlertIncident = { ...openIncident, status: "acknowledged", acknowledged_by: "gokay" }
    const fetchMock = stubBaseFetch((url) => {
      if (url.endsWith("/acknowledge")) return { ok: true, json: async () => acknowledged } as Response
      return undefined
    })
    vi.stubGlobal("fetch", fetchMock)

    const onIncidentUpdated = vi.fn()
    render(
      <SessionProvider>
        <AlarmCenter incidents={[openIncident]} onIncidentUpdated={onIncidentUpdated} />
      </SessionProvider>,
    )
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/session"))

    expect(screen.queryByLabelText("Yetkili token")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "incident-01 olayını onayla" })).toBeDisabled()

    fireEvent.change(screen.getByPlaceholderText("Ad veya kimlik"), { target: { value: "gokay" } })
    expect(screen.getByRole("button", { name: "incident-01 olayını onayla" })).toBeEnabled()
    fireEvent.click(screen.getByRole("button", { name: "incident-01 olayını onayla" }))

    await waitFor(() => expect(onIncidentUpdated).toHaveBeenCalledWith(acknowledged))
    const ackCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/acknowledge"))
    expect(ackCall).toBeDefined()
    const headers = new Headers((ackCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
    expect(headers.has("Authorization")).toBe(false)
  })

  it("creates an alert rule with the session's CSRF header", async () => {
    const createdRule: AlertRule = { id: "rule-99", name: "Yüksek CPU", kind: "metric", metric: "cpu", threshold: 90, stale_after_seconds: 0, severity: "critical", enabled: true, created_at: "2026-09-22T22:00:00Z" }
    const fetchMock = stubBaseFetch((url, init) => {
      if (url.endsWith("/api/v1/alert-rules") && init?.method === "POST") return { ok: true, json: async () => createdRule } as Response
      return undefined
    })
    vi.stubGlobal("fetch", fetchMock)

    render(
      <SessionProvider>
        <AlarmCenter incidents={[]} onIncidentUpdated={() => undefined} />
      </SessionProvider>,
    )
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/session"))

    fireEvent.change(screen.getByPlaceholderText("Ad veya kimlik"), { target: { value: "gokay" } })
    // "Ad" appears twice (rule form and maintenance-window form both have a
    // field with that label) — getAllByLabelText avoids the ambiguity
    // getByLabelText would raise; the rule form is first in DOM order.
    fireEvent.change(screen.getAllByLabelText("Ad")[0], { target: { value: "Yüksek CPU" } })
    fireEvent.click(screen.getByRole("button", { name: "Kuralı kaydet" }))

    await waitFor(() => expect(screen.getByText("Yüksek CPU")).toBeInTheDocument())
    const createCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/api/v1/alert-rules") && (call[1] as RequestInit)?.method === "POST")
    expect(createCall).toBeDefined()
    const headers = new Headers((createCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
  })
})
