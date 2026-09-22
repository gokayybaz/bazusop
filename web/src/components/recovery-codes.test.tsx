import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { RecoveryCodes } from "./recovery-codes"

describe("RecoveryCodes", () => {
  it("renders every code and a one-time-display warning", () => {
    render(<RecoveryCodes codes={["aaaa1111", "bbbb2222", "cccc3333"]} />)

    const list = screen.getByRole("list", { name: "Kurtarma kodları" })
    expect(list.children).toHaveLength(3)
    expect(screen.getByText("aaaa1111")).toBeInTheDocument()
    expect(screen.getByText("bbbb2222")).toBeInTheDocument()
    expect(screen.getByText("cccc3333")).toBeInTheDocument()
    expect(screen.getByText(/yalnız bir kez gösterilir/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Tümünü kopyala" })).toBeInTheDocument()
  })
})
