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
})
