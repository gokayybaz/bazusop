import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import { LandingPage } from "../src/landing-page"
import "./styles.css"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <LandingPage />
  </StrictMode>,
)
