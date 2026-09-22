import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { TotpQrCode } from "./totp-qr-code"

describe("TotpQrCode", () => {
  it("renders a scannable SVG QR code for the provisioning URI", async () => {
    render(<TotpQrCode provisioningUri="otpauth://totp/bazUSOP:admin@example.com?secret=JBSWY3DPEHPK3PXP&issuer=bazUSOP&digits=6&period=30" />)

    const image = await screen.findByRole("img", { name: "Doğrulayıcı uygulaması QR kodu" })
    expect(image.querySelector("svg")).toBeInTheDocument()
  })
})
