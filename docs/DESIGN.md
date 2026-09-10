# bazUSOP design system

## Product character

bazUSOP communicates technical reliability, operational control and calm under
pressure. It is a working surface for infrastructure teams, not a social or
entertainment product. The visual density resembles a simplified BI console:
important state is visible immediately, while decoration stays quiet.

## Color system

Dark is the default operational theme. Light remains selectable for operator
preference and accessibility.

| Token | Dark | Purpose |
| --- | --- | --- |
| Background | `#121212` | Primary application canvas |
| Card | `#1E1E1E` | Panels and KPI surfaces |
| Border | `#2C2C2E` | Dividers, grids and table rows |
| Data accent | `#00E5FF` | Primary charts, key values and actions |
| Critical | `#FF453A` | Critical alerts and destructive state |
| Healthy | `#32D74B` | Live and normal state |
| Primary text | `#FFFFFF` | Titles and important values |
| Secondary text | `#98989D` | Labels and metadata |

Accent colors are semantic and sparse. Cyan is reserved for data and active
control, green for healthy/live state, amber for warnings and red for critical
conditions. Pastel decoration is not part of the product language.

## Typography

- Interface and body: Inter/system sans, regular, 14px minimum.
- Page title: Inter/system sans, semibold, 24–32px.
- KPIs and machine values: JetBrains Mono/Fira Code/system mono, bold, 32px.
- Secondary metadata may use 12–13px when it is not operationally critical.

## Geometry and spacing

- Use a 12-column responsive grid.
- Space in 8px multiples: 8, 16, 24 and 32px.
- Cards use a 16px radius, 20px internal padding and a subtle 1px border.
- Tables use quiet row dividers and a `#252525` dark-theme hover surface.
- The dashboard starts with three KPIs. The main workspace divides into an
  eight-column operational view and a four-column alerts panel.

## Data visualization

- Prefer area charts when showing volatile resource usage.
- Use subtle `#2C2C2E` grid lines without heavy chart frames.
- Use cyan for the primary series and green for healthy comparison or area fill.
- Always pair color with text, shape or status labels; color is never the sole signal.

## Alerts and interaction

- Critical alert surfaces use a restrained dark-red tint, critical red text and
  a 4px red leading border.
- Motion uses 150–180ms easing and respects `prefers-reduced-motion`.
- Focus, hover and selected states must remain distinguishable in both themes.
- Operational actions use domain icons such as servers, terminals, services and
  activity signals; decorative or metaphorically vague icons are avoided.

