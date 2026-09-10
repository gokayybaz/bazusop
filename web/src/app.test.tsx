import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { App } from "./app"

describe("bazUSOP shell", () => {
  it("presents the unified operations overview", () => {
    render(<App />)

    expect(screen.getByRole("heading", { name: "Operations overview" })).toBeInTheDocument()
    expect(screen.getByText("Instances")).toBeInTheDocument()
    expect(screen.getByText("Healthy systems")).toBeInTheDocument()
    expect(screen.getByText("Open alerts")).toBeInTheDocument()
  })
})

