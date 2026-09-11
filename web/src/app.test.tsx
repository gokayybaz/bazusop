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

    expect(screen.getByRole("heading", { name: "Operasyon özeti" })).toBeInTheDocument()
    expect(screen.getByText("Sunucular")).toBeInTheDocument()
    expect(screen.getByText("Ortalama CPU")).toBeInTheDocument()
    expect(screen.getByText("Açık alarmlar")).toBeInTheDocument()
    expect(screen.getByLabelText("Filo özeti").children).toHaveLength(3)
    expect(screen.getByRole("region", { name: "Operasyon alarmları" })).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Son 24 saatte filo kaynak kullanımı" })).toBeInTheDocument()
    expect(screen.getByRole("table", { name: "Sunucu sağlığı" })).toBeInTheDocument()
  })

  it("lets the operator select and persist a dark theme variant", () => {
    render(<App />)

    fireEvent.click(screen.getByRole("button", { name: "Gece temasını kullan" }))

    expect(document.documentElement.dataset.theme).toBe("midnight")
    expect(window.localStorage.getItem("bazusop-theme")).toBe("midnight")
    expect(screen.getByRole("button", { name: "Grafit temasını kullan" })).toBeInTheDocument()
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
	} as Response).mockResolvedValueOnce({
	  ok: true,
	  json: async () => ({
		latest: {
		  recorded_at: "2026-09-11T04:05:00Z",
		  cpu_percent: 47.8,
		  memory_percent: 63.4,
		  disk_percent: 71.1,
		  network_rx_bytes: 2048,
		  network_tx_bytes: 1024,
		},
		samples: [
		  { recorded_at: "2026-09-11T04:00:00Z", cpu_percent: 42.5, memory_percent: 62.1, disk_percent: 71, network_rx_bytes: 1024, network_tx_bytes: 512 },
		  { recorded_at: "2026-09-11T04:05:00Z", cpu_percent: 47.8, memory_percent: 63.4, disk_percent: 71.1, network_rx_bytes: 2048, network_tx_bytes: 1024 },
		],
	  }),
	} as Response).mockResolvedValueOnce({
	  ok: true,
	  json: async () => ({
		services: [
		  { agent_id: "agent-01", name: "nginx.service", display_name: "NGINX Web Server", state: "running", startup_type: "automatic", observed_at: "2026-09-11T04:05:00Z" },
		  { agent_id: "agent-01", name: "queue-worker.service", display_name: "Queue Worker", state: "failed", startup_type: "automatic", observed_at: "2026-09-11T04:05:00Z" },
		],
	  }),
	} as Response)

	render(<App />)

	expect(await screen.findByText("edge-01.example.com")).toBeInTheDocument()
	expect(screen.getByText("Ubuntu 24.04")).toBeInTheDocument()
	expect(screen.getByText("8 çekirdek · 16 GiB")).toBeInTheDocument()
	expect(screen.getByText("10.0.0.8")).toBeInTheDocument()

	fireEvent.click(screen.getByRole("button", { name: "edge-01.example.com ayrıntılarını aç" }))

	expect(await screen.findByRole("region", { name: "edge-01.example.com telemetrisi" })).toBeInTheDocument()
	expect(screen.getByText("47.8%")).toBeInTheDocument()
	expect(screen.getByRole("img", { name: "Son 24 saat CPU ve bellek kullanımı" })).toBeInTheDocument()
	expect(await screen.findByRole("region", { name: "edge-01.example.com servisleri" })).toBeInTheDocument()
	expect(screen.getByText("NGINX Web Server")).toBeInTheDocument()
	expect(screen.getByText("Queue Worker")).toBeInTheDocument()

	fireEvent.click(screen.getByRole("button", { name: "Başarısız servisleri göster" }))
	expect(screen.queryByText("NGINX Web Server")).not.toBeInTheDocument()
	expect(screen.getByText("Queue Worker")).toBeInTheDocument()
  })
})
