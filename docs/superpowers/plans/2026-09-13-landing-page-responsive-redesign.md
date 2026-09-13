# Landing Page Responsive Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a compact, professional bazUSOP landing page whose content and layout remain stable from 320 px through wide desktop viewports while preserving the independent GitHub Pages build.

**Architecture:** Keep the existing standalone Vite landing entry and replace only the landing React composition and stylesheet. The component owns semantic content and the static product visualization; the stylesheet owns a single fluid container system, intrinsic grids, progressive dashboard simplification, and responsive behavior without fixed section heights.

**Tech Stack:** React 19, TypeScript 5.9, Vite 7, Vitest 3, Testing Library, Lucide React, CSS

**Spec:** `docs/superpowers/specs/2026-09-13-landing-page-responsive-redesign.md`

## Global Constraints

- Keep `web/landing/index.html`, `web/landing/main.tsx`, and `web/vite.landing.config.ts` as the separate GitHub Pages entry and build configuration.
- Do not add runtime dependencies or external font requests.
- Keep the landing page functional as static files with no API dependency.
- Keep application routes and the normal `npm run build` output unchanged.
- Keep meaningful content inside the document width at 320, 390, 768, 1024, 1280, and 1440 px.
- Keep the mobile hero heading at or below 46 px.
- Avoid fixed `min-height` values on content sections and child `min-width` values that control parent layout.

---

### Task 1: Replace the Landing Content Hierarchy

**Files:**
- Modify: `web/src/app.test.tsx`
- Modify: `web/src/landing-page.tsx`

**Interfaces:**
- Consumes: the existing exported `LandingPage(): JSX.Element` component and the repository URL `https://github.com/gokayybaz/bazusop`
- Produces: semantic landmarks named `Landing page navigasyonu`, `Canlı operasyon görünümü`, `Platform kabiliyetleri`, `Mimari`, and `Güvenlik`

- [ ] **Step 1: Write the failing public-page contract test**

Replace the existing landing-page test with:

```tsx
it("presents a verifiable public product story", () => {
  render(<LandingPage />)

  expect(screen.getByRole("heading", { name: "Sunucu operasyonları için tek çalışma yüzeyi." })).toBeInTheDocument()
  expect(screen.getByRole("navigation", { name: "Landing page navigasyonu" })).toBeInTheDocument()
  expect(screen.getByRole("region", { name: "Canlı operasyon görünümü" })).toBeInTheDocument()
  expect(screen.getByRole("list", { name: "Platform kabiliyetleri" }).children).toHaveLength(4)
  expect(screen.getByRole("heading", { name: "Sinyalden müdahaleye, bağlam kaybetmeden." })).toBeInTheDocument()
  expect(screen.getByRole("heading", { name: "Kimlikten başlayan güvenlik." })).toBeInTheDocument()
  expect(screen.getAllByRole("link", { name: /GitHub'da incele/ })[0]).toHaveAttribute(
    "href",
    "https://github.com/gokayybaz/bazusop",
  )
})
```

- [ ] **Step 2: Run the focused test and observe the expected failure**

Run:

```bash
cd web && npm test -- --run src/app.test.tsx -t "presents a verifiable public product story"
```

Expected: FAIL because the revised hero heading and named regions do not exist yet.

- [ ] **Step 3: Implement the revised semantic structure**

In `web/src/landing-page.tsx`:

- Replace `productCapabilities` with three unnumbered records using `Activity`, `TerminalSquare`, and `Cloud`.
- Add four verified facts: `Linux + Windows`, `Agent tabanlı kimlik`, `İmzalı işler`, and `AWS · Azure · GCP`.
- Render a compact header with the three in-page links and a labeled `GitHub'da incele` action.
- Use the exact hero heading `Sunucu operasyonları için tek çalışma yüzeyi.` and keep one primary GitHub action plus one architecture anchor.
- Give the product preview `role="region"` and `aria-label="Canlı operasyon görünümü"`.
- Render the facts in a `ul` with `aria-label="Platform kabiliyetleri"`.
- Render three unnumbered capability articles under the exact heading `Sinyalden müdahaleye, bağlam kaybetmeden.`.
- Keep the agent-to-control-plane-to-provider architecture explanation under an `id="architecture"` section.
- Render the security explanation under the exact heading `Kimlikten başlayan güvenlik.`.
- Keep a compact final CTA and footer without decorative orbit elements.

The page remains a named export:

```tsx
export function LandingPage() {
  return (
    <div className="landing-shell">
      <header className="landing-header">...</header>
      <main>
        <section className="landing-hero">...</section>
        <section className="landing-facts" aria-label="Ürün kapsamı">...</section>
        <section className="landing-section capabilities" id="platform">...</section>
        <section className="architecture" id="architecture">...</section>
        <section className="landing-section security" id="security">...</section>
        <section className="landing-cta">...</section>
      </main>
      <footer className="landing-footer">...</footer>
    </div>
  )
}
```

- [ ] **Step 4: Run the focused test and observe it pass**

Run:

```bash
cd web && npm test -- --run src/app.test.tsx -t "presents a verifiable public product story"
```

Expected: PASS.

- [ ] **Step 5: Commit the content hierarchy**

```bash
git add web/src/app.test.tsx web/src/landing-page.tsx
git commit -m "feat: refine landing page product story"
```

### Task 2: Build the Fluid Responsive Design System

**Files:**
- Modify: `web/landing/styles.css`

**Interfaces:**
- Consumes: the class names and section hierarchy produced by Task 1
- Produces: a width-safe layout based on `--page-gutter`, `--content-max`, `.landing-container`, intrinsic grids, and two progressive breakpoints

- [ ] **Step 1: Record the failing layout measurements**

Start the landing server:

```bash
cd web && npm run dev:landing -- --host 127.0.0.1
```

In the collaborative browser, measure the page at 820, 821, 1180, and 1181 px with:

```js
(() => ({
  clientWidth: document.documentElement.clientWidth,
  scrollWidth: document.documentElement.scrollWidth,
  scrollHeight: document.documentElement.scrollHeight,
  heroHeight: document.querySelector(".landing-hero")?.getBoundingClientRect().height,
  capabilityHeight: document.querySelector(".capability-list")?.getBoundingClientRect().height,
}))()
```

Expected before the rewrite: the hero changes by about 369 px across 1180/1181 and the capability list changes by about 606 px across 820/821.

- [ ] **Step 2: Replace the styling with a compact token system**

Define the page foundation in `web/landing/styles.css`:

```css
:root {
  color-scheme: dark;
  --page-bg: #090d10;
  --surface-1: #0e1418;
  --surface-2: #121a1f;
  --line: rgba(197, 214, 218, 0.14);
  --line-strong: rgba(197, 214, 218, 0.24);
  --text: #edf2f3;
  --muted: #96a5a9;
  --accent: #4fd1c5;
  --accent-soft: rgba(79, 209, 197, 0.1);
  --warning: #e7b66c;
  --critical: #ee8178;
  --content-max: 1280px;
  --page-gutter: clamp(20px, 4vw, 64px);
}

.landing-container {
  width: min(calc(100% - (2 * var(--page-gutter))), var(--content-max));
  margin-inline: auto;
}
```

Use system sans-serif for normal content and monospace only for operational values. Remove Georgia, perspective transforms, full-section fixed minimum heights, glow-heavy box shadows, and the decorative CTA orbit.

- [ ] **Step 3: Implement intrinsic section layouts**

Use a stable hero grid and intrinsic supporting grids:

```css
.landing-hero__inner {
  display: grid;
  grid-template-columns: minmax(0, 0.82fr) minmax(520px, 1.18fr);
  gap: clamp(40px, 6vw, 88px);
  align-items: center;
  padding-block: clamp(64px, 8vw, 112px);
}

.landing-facts__list,
.capability-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 230px), 1fr));
}

.landing-hero h1 {
  font-size: clamp(42px, 5.1vw, 72px);
  line-height: 1.02;
}
```

Keep the product surface flat, use readable 11–14 px interface labels, and let its grid shrink with `minmax(0, 1fr)`. At narrow widths, remove only secondary chart/list detail; do not hide the product identity or status.

- [ ] **Step 4: Add progressive responsive rules**

Use one layout breakpoint and one compact-screen breakpoint:

```css
@media (max-width: 1040px) {
  .landing-hero__inner,
  .architecture,
  .security {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .landing-nav { display: none; }
  .landing-hero h1 { font-size: clamp(38px, 11.5vw, 46px); }
  .hero-actions { align-items: stretch; flex-direction: column; }
  .landing-facts__list,
  .capability-list { grid-template-columns: 1fr; }
}

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
  }
}
```

- [ ] **Step 5: Re-run the layout measurements**

Measure at 820, 821, 1040, and 1041 px using the Step 1 browser expression.

Expected: `scrollWidth === clientWidth` at every width; the 820/821 measurements remain within 24 px of each other; and the 1040/1041 change reflects only the intentional one-to-two-column hero transition without any child overflow.

- [ ] **Step 6: Commit the responsive system**

```bash
git add web/landing/styles.css
git commit -m "fix: stabilize landing page layouts"
```

### Task 3: Verify Static Publishing and All Target Viewports

**Files:**
- Verify: `web/landing/index.html`
- Verify: `web/landing/main.tsx`
- Verify: `web/vite.landing.config.ts`
- Verify: `.github/workflows/pages.yml`

**Interfaces:**
- Consumes: the landing component and stylesheet from Tasks 1 and 2
- Produces: a verified `web/landing-dist/` GitHub Pages artifact without changing the normal application build contract

- [ ] **Step 1: Run the full component test suite**

```bash
cd web && npm test
```

Expected: all tests pass with zero failures.

- [ ] **Step 2: Build the application and landing artifacts independently**

```bash
cd web && npm run build
cd web && npm run build:landing
```

Expected: both commands exit with code 0; the landing build emits `web/landing-dist/index.html` and hashed assets.

- [ ] **Step 3: Inspect all required viewports**

At 320, 390, 768, 1024, 1280, and 1440 px, capture a browser screenshot and evaluate:

```js
(() => {
  const width = document.documentElement.clientWidth
  const offenders = [...document.querySelectorAll("body *")]
    .map((element) => {
      const rect = element.getBoundingClientRect()
      return { element, left: rect.left, right: rect.right }
    })
    .filter(({ element, left, right }) => {
      if (element.getAttribute("aria-hidden") === "true") return false
      return left < -0.5 || right > width + 0.5
    })

  return {
    clientWidth: width,
    scrollWidth: document.documentElement.scrollWidth,
    offenderCount: offenders.length,
  }
})()
```

Expected: `scrollWidth === clientWidth`, `offenderCount === 0`, readable product labels, no clipped CTA, and no isolated single-word hero line.

- [ ] **Step 4: Run repository hygiene checks**

```bash
git diff --check
git status --short
```

Expected: no whitespace errors; only intentional landing, publishing, documentation, and test files are modified or untracked.

- [ ] **Step 5: Commit any final verification adjustment**

If visual inspection required a correction, stage only the corrected landing files and commit:

```bash
git add web/src/landing-page.tsx web/landing/styles.css web/src/app.test.tsx
git commit -m "fix: polish landing page viewport behavior"
```

If no correction was required, do not create an empty commit.
