import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { JobPanel } from "./job-panel"
import { SessionProvider } from "../lib/session"
import type { InventoryInstance, OperationJob } from "../types"

const instance: InventoryInstance = {
  agent_id: "agent-01", hostname: "edge-01.example.com", os_family: "linux", os_name: "Ubuntu", os_version: "24.04",
  architecture: "amd64", kernel_version: "6.8.0", cpu_cores: 4, memory_bytes: 8589934592, ip_addresses: ["10.0.0.8"],
  agent_version: "0.3.0", first_seen_at: "2026-09-11T04:00:00Z", last_seen_at: "2026-09-11T04:05:00Z", status: "connected",
}

const authenticatedWhoAmI = { user_id: "user-1", email: "operator@example.com", role: "operator", csrf_token: "csrf-token-abc", expires_at: "2026-09-22T22:00:00Z" }

describe("JobPanel", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("has no operator-token field and submits job creation with the session's CSRF header", async () => {
    const createdJob: OperationJob = {
      id: "job-01", agent_id: "agent-01", action: "service.restart", target: "nginx.service", approved_by: "gokay",
      reason: "config rollout", requested_at: "2026-09-11T04:06:00Z", status: "queued", last_sequence: 0, signature: "", signing_public_key: "",
    }
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/session")) return Promise.resolve({ ok: true, json: async () => authenticatedWhoAmI } as Response)
      if (url.endsWith("/jobs")) return Promise.resolve({ ok: true, json: async () => createdJob } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    const onCreated = vi.fn()
    render(
      <SessionProvider>
        <JobPanel instance={instance} jobs={[]} onCreated={onCreated} state="ready" />
      </SessionProvider>,
    )
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/v1/session"))

    fireEvent.click(screen.getByRole("button", { name: "Yeni iş oluştur" }))
    expect(screen.queryByLabelText("Operatör token'ı")).not.toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText("nginx.service"), { target: { value: "nginx.service" } })
    fireEvent.change(screen.getByPlaceholderText("Operatör kimliği"), { target: { value: "gokay" } })
    fireEvent.change(screen.getByPlaceholderText("Bu aksiyon neden gerekli?"), { target: { value: "config rollout" } })
    fireEvent.click(screen.getByRole("button", { name: "Onayı kaydet ve sıraya al" }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(createdJob))
    const jobCall = fetchMock.mock.calls.find((call) => String(call[0]).endsWith("/jobs"))
    expect(jobCall).toBeDefined()
    const headers = new Headers((jobCall?.[1] as RequestInit).headers)
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token-abc")
    expect(headers.has("Authorization")).toBe(false)
  })
})
