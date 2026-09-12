import { useEffect, useState } from "react"
import { Moon, Sun } from "lucide-react"

type Theme = "light" | "dark"

const storageKey = "bazusop-theme"

function initialTheme(): Theme {
  const persisted = window.localStorage.getItem(storageKey)
  return persisted === "light" ? "light" : "dark"
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(initialTheme)
  const nextTheme = theme === "dark" ? "light" : "dark"
  const nextThemeLabel = nextTheme === "light" ? "Açık" : "Koyu"

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.documentElement.style.colorScheme = theme
    window.localStorage.setItem(storageKey, theme)
  }, [theme])

  return (
    <button
      aria-label={`${nextThemeLabel} temayı kullan`}
      className="theme-toggle"
      onClick={() => setTheme(nextTheme)}
      title={`${nextThemeLabel} temayı kullan`}
      type="button"
    >
      <span className="theme-toggle-track" aria-hidden="true">
        <Sun size={14} />
        <Moon size={14} />
        <i className={theme} />
      </span>
    </button>
  )
}
