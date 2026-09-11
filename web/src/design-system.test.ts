import { describe, expect, it } from "vitest"
// @ts-expect-error Node's built-in module is provided by the Vitest runtime.
import { readFileSync } from "node:fs"

declare const process: { cwd: () => string }

const styles = readFileSync(`${process.cwd()}/src/styles.css`, "utf8")
const documentShell = readFileSync(`${process.cwd()}/index.html`, "utf8")

describe("bazUSOP design system", () => {
  it("does not use colored leading borders", () => {
    expect(styles).toContain(".alerts-card")
    expect(styles).not.toMatch(/border-left(?:-color)?\s*:/)
    expect(styles).not.toMatch(/box-shadow:\s*inset/)
  })

  it("declares Turkish as the product's primary language", () => {
    expect(documentShell).toContain('<html lang="tr">')
    expect(documentShell).toContain("Linux ve Windows sunucuları")
  })
})
