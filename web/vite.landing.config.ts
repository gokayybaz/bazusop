import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

export default defineConfig({
  root: new URL("./landing", import.meta.url).pathname,
  base: "./",
  plugins: [react(), tailwindcss()],
  build: {
    emptyOutDir: true,
    outDir: new URL("../landing-dist", import.meta.url).pathname,
  },
})
