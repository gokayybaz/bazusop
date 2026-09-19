export function ServiceFilterButton({ active, label, onClick, accessibleLabel }: {
  active: boolean
  label: string
  onClick: () => void
  accessibleLabel?: string
}) {
  return <button aria-label={accessibleLabel} aria-pressed={active} className={active ? "service-filter active" : "service-filter"} onClick={onClick} type="button">{label}</button>
}
