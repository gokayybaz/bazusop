import { BrowserRouter, Route, Routes } from "react-router-dom"

import { AppShell } from "./app-shell"
import { ProtectedRoute } from "./lib/protected-route"
import { SessionProvider } from "./lib/session"
import { LoginPage } from "./pages/login"

export function App() {
  return (
    <BrowserRouter>
      <SessionProvider>
        <Routes>
          <Route element={<LoginPage />} path="/login" />
          <Route
            element={
              <ProtectedRoute>
                <AppShell />
              </ProtectedRoute>
            }
            path="/*"
          />
        </Routes>
      </SessionProvider>
    </BrowserRouter>
  )
}
