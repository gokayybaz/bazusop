import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter, Route, Routes } from "react-router-dom"

import { InvitePage } from "./invite"

function renderAtToken(token: string) {
  return render(
    <MemoryRouter initialEntries={[`/invite/${token}`]}>
      <Routes>
        <Route element={<InvitePage />} path="/invite/:token" />
      </Routes>
    </MemoryRouter>,
  )
}

describe("InvitePage", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("walks through password, TOTP confirmation, and recovery codes", async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/consume")) return Promise.resolve({
        ok: true,
        status: 201,
        json: async () => ({ id: "user-1", provisioning_uri: "otpauth://totp/bazUSOP:new-user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=bazUSOP&digits=6&period=30" }),
      } as Response)
      if (url.includes("/confirm-totp")) return Promise.resolve({
        ok: true,
        status: 200,
        json: async () => ({ recovery_codes: ["aaaa1111", "bbbb2222"] }),
      } as Response)
      return Promise.resolve({ ok: false } as Response)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderAtToken("token-abc")

    expect(screen.getByRole("heading", { name: "Hesabınızı oluşturun" })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Parola"), { target: { value: "a brand new password" } })
    fireEvent.click(screen.getByRole("button", { name: "Devam et" }))

    expect(await screen.findByRole("heading", { name: "Doğrulayıcı uygulamasını bağla" })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole("img", { name: "Doğrulayıcı uygulaması QR kodu" }).querySelector("svg")).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText("Doğrulayıcı kodu"), { target: { value: "123456" } })
    fireEvent.click(screen.getByRole("button", { name: "Kodu doğrula" }))

    expect(await screen.findByRole("heading", { name: "Hesabınız hazır" })).toBeInTheDocument()
    expect(screen.getByText("aaaa1111")).toBeInTheDocument()

    const consumeCall = fetchMock.mock.calls.find((call) => String(call[0]).includes("/consume"))
    expect(consumeCall?.[0]).toBe("/api/v1/invites/token-abc/consume")
  })

  it("shows an error when the invite token is unknown", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 404 } as Response))

    renderAtToken("unknown-token")

    fireEvent.change(screen.getByLabelText("Parola"), { target: { value: "whatever password" } })
    fireEvent.click(screen.getByRole("button", { name: "Devam et" }))

    expect(await screen.findByText("Bu davet bulunamadı.")).toBeInTheDocument()
  })
})
