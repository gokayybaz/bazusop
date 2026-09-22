import type { ReactNode } from "react"
import { Navigate } from "react-router-dom"

import { useSession } from "./session"

export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { state } = useSession()
  if (state.status === "loading") {
    return (
      <div className="auth-loading" role="status">
        Yükleniyor…
      </div>
    )
  }
  if (state.status === "anonymous") return <Navigate replace to="/login" />
  return <>{children}</>
}
