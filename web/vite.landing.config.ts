import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

export default defineConfig({
  root: new URL("./landing", import.meta.url).pathname,
  base: "./",
  publicDir: new URL("./public", import.meta.url).pathname,
  plugins: [react(), tailwindcss()],
  build: {
    emptyOutDir: true,
    outDir: new URL("../landing-dist", import.meta.url).pathname,
  },
})
