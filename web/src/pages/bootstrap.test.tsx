import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"

import { BootstrapPage } from "./bootstrap"

describe("BootstrapPage", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("completes setup and shows the QR code and recovery codes", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({
        provisioning_uri: "otpauth://totp/bazUSOP:admin@example.com?secret=JBSWY3DPEHPK3PXP&issuer=bazUSOP&digits=6&period=30",
        recovery_codes: ["aaaa1111", "bbbb2222"],
      }),
    } as Response))

    render(
      <MemoryRouter initialEntries={["/setup"]}>
        <BootstrapPage />
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByLabelText("Kurulum secret'ı"), { target: { value: "verify-bootstrap" } })
    fireEvent.change(screen.getByLabelText("Yönetici e-postası"), { target: { value: "admin@example.com" } })
    fireEvent.change(screen.getByLabelText("Parola"), { target: { value: "correct horse battery staple" } })
    fireEvent.click(screen.getByRole("button", { name: "Kurulumu tamamla" }))

    expect(await screen.findByRole("heading", { name: "Kurulum tamamlandı" })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole("img", { name: "Doğrulayıcı uygulaması QR kodu" }).querySelector("svg")).toBeInTheDocument())
    expect(screen.getByText("aaaa1111")).toBeInTheDocument()
  })

  it("shows an already-bootstrapped message with a link to login on 409", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 409 } as Response))

    render(
      <MemoryRouter initialEntries={["/setup"]}>
        <BootstrapPage />
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByLabelText("Kurulum secret'ı"), { target: { value: "wrong" } })
    fireEvent.change(screen.getByLabelText("Yönetici e-postası"), { target: { value: "admin@example.com" } })
    fireEvent.change(screen.getByLabelText("Parola"), { target: { value: "correct horse battery staple" } })
    fireEvent.click(screen.getByRole("button", { name: "Kurulumu tamamla" }))

    expect(await screen.findByRole("heading", { name: "Hub zaten kurulu" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Girişe git" })).toHaveAttribute("href", "/login")
  })
})
