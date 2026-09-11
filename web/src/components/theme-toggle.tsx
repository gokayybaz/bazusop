import { useEffect, useState } from "react"
import { Contrast, Moon } from "lucide-react"

type Theme = "graphite" | "midnight"

const storageKey = "bazusop-theme"

function initialTheme(): Theme {
  const persisted = window.localStorage.getItem(storageKey)
  return persisted === "midnight" ? "midnight" : "graphite"
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(initialTheme)
  const nextTheme = theme === "graphite" ? "midnight" : "graphite"
  const nextThemeLabel = nextTheme === "midnight" ? "Gece" : "Grafit"

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.documentElement.style.colorScheme = "dark"
    window.localStorage.setItem(storageKey, theme)
  }, [theme])

  return (
    <button
      aria-label={`${nextThemeLabel} temasını kullan`}
      className="theme-toggle"
      onClick={() => setTheme(nextTheme)}
      title={`${nextThemeLabel} temasını kullan`}
      type="button"
    >
      <span className="theme-toggle-track" aria-hidden="true">
        <Contrast size={14} />
        <Moon size={14} />
        <i className={theme} />
      </span>
    </button>
  )
}
