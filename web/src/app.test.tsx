import { fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { App } from "./app"

describe("bazUSOP shell", () => {
  beforeEach(() => {
    window.localStorage.clear()
    delete document.documentElement.dataset.theme
	vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
	  ok: true,
	  json: async () => ({ instances: [] }),
	}))
  })

  afterEach(() => vi.unstubAllGlobals())

  it("presents the unified operations overview", () => {
    render(<App />)

    expect(screen.getByRole("heading", { name: "Operations overview" })).toBeInTheDocument()
    expect(screen.getByText("Instances")).toBeInTheDocument()
    expect(screen.getByText("Average CPU")).toBeInTheDocument()
    expect(screen.getByText("Open alerts")).toBeInTheDocument()
    expect(screen.getByLabelText("Fleet summary").children).toHaveLength(3)
    expect(screen.getByRole("region", { name: "Operational alerts" })).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Fleet resource utilization over 24 hours" })).toBeInTheDocument()
    expect(screen.getByRole("table", { name: "Instance health" })).toBeInTheDocument()
  })

  it("lets the operator select and persist a dark theme variant", () => {
    render(<App />)

    fireEvent.click(screen.getByRole("button", { name: "Use midnight theme" }))

    expect(document.documentElement.dataset.theme).toBe("midnight")
    expect(window.localStorage.getItem("bazusop-theme")).toBe("midnight")
    expect(screen.getByRole("button", { name: "Use graphite theme" })).toBeInTheDocument()
  })

  it("renders enrolled instances returned by the inventory API", async () => {
	vi.mocked(fetch).mockResolvedValueOnce({
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

	render(<App />)

	expect(await screen.findByText("edge-01.example.com")).toBeInTheDocument()
	expect(screen.getByText("Ubuntu 24.04")).toBeInTheDocument()
	expect(screen.getByText("8 cores · 16 GiB")).toBeInTheDocument()
	expect(screen.getByText("10.0.0.8")).toBeInTheDocument()
  })
})
