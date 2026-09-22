import { existsSync, readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"

declare const process: { cwd: () => string }

const entrypoints = [
  { name: "uygulama", path: "index.html" },
  { name: "landing page", path: "landing/index.html" },
]

describe("bazUSOP favicon", () => {
  it.each(entrypoints)("$name ortak marka ikonunu kullanır", ({ path }) => {
    const html = readFileSync(`${process.cwd()}/${path}`, "utf8")
    const document = new DOMParser().parseFromString(html, "text/html")
    const favicon = document.querySelector<HTMLLinkElement>('link[rel="icon"]')

    expect(favicon).not.toBeNull()
    expect(favicon?.getAttribute("type")).toBe("image/svg+xml")
    expect(favicon?.getAttribute("href")).toMatch(/favicon\.svg$/)
    expect(existsSync(`${process.cwd()}/public/favicon.svg`)).toBe(true)
  })
})
