import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it } from "vitest"

import { App } from "./app"

describe("bazUSOP shell", () => {
  beforeEach(() => {
    window.localStorage.clear()
    delete document.documentElement.dataset.theme
  })

  it("presents the unified operations overview", () => {
    render(<App />)

    expect(screen.getByRole("heading", { name: "Operations overview" })).toBeInTheDocument()
    expect(screen.getByText("Instances")).toBeInTheDocument()
    expect(screen.getByText("Healthy systems")).toBeInTheDocument()
    expect(screen.getByText("Open alerts")).toBeInTheDocument()
    expect(screen.getByLabelText("Fleet summary").children).toHaveLength(3)
    expect(screen.getByRole("region", { name: "Operational alerts" })).toBeInTheDocument()
  })

  it("lets the operator select and persist the light theme", () => {
    render(<App />)

    fireEvent.click(screen.getByRole("button", { name: "Use light theme" }))

    expect(document.documentElement.dataset.theme).toBe("light")
    expect(window.localStorage.getItem("bazusop-theme")).toBe("light")
    expect(screen.getByRole("button", { name: "Use dark theme" })).toBeInTheDocument()
  })
})
