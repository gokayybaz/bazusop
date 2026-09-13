# bazUSOP Landing Page Responsive Redesign

## Context

The public landing page is built separately from the application and published as a static GitHub Pages artifact. The current page has no persistent horizontal overflow, but its rigid layout rules create visible jumps: the hero changes from roughly 780 px to 1,149 px at the 1,180 px breakpoint, and the capability grid changes from roughly 385 px to 991 px at the 820 px breakpoint. On mobile, large headings, fixed minimum heights, and oversized section spacing make the page feel unstable and unnecessarily long.

## Goal

Rework the landing page into a restrained, professional operations product presentation that remains visually stable and readable from 320 px mobile screens through wide desktop layouts, while preserving the independent GitHub Pages build.

## Visual Direction

- Preserve the dark technical character, but replace the neon-heavy SaaS look with graphite, steel, and restrained turquoise accents.
- Use the system sans-serif stack for primary typography and monospace only for operational metadata.
- Avoid italic headline fragments, decorative all-caps eyebrows, numbered feature cards, repeated arrow links, and unsupported success metrics.
- Use borders, alignment, and measured spacing instead of glow, perspective transforms, or large ornamental circles.
- Present the product preview as a readable, flat operational surface rather than a decorative 3D mockup.

## Information Architecture

1. Header with brand, in-page navigation, and a clearly labeled GitHub action.
2. Compact hero with one primary value proposition, one supporting paragraph, two actions, and a readable product surface.
3. Product facts strip containing verifiable capabilities instead of invented performance claims.
4. Three capability narratives covering observation, controlled intervention, and hybrid inventory.
5. Architecture section showing the agent-to-control-plane-to-provider flow.
6. Security section explaining identity, transport, enrollment, and signed jobs.
7. Compact final call to action and footer.

## Responsive Layout

- Use one shared content container with fluid gutters: `clamp(20px, 4vw, 64px)` and a maximum content width of 1,280 px.
- Avoid fixed `min-height` values on content sections and avoid child `min-width` values that control parent layout.
- Keep the hero in two columns only while both columns remain readable; switch to one column before either column compresses.
- Use fluid type scales that keep the mobile hero heading at or below 46 px and prevent single-word line fragments where practical.
- Let capability and fact grids wrap with `repeat(auto-fit, minmax(...))` so one-pixel viewport changes do not double section height unexpectedly.
- Progressively simplify the product surface on narrow screens while preserving meaningful labels and values.
- Keep every meaningful element inside the document width at 320, 390, 768, 1024, 1280, and 1440 px viewports.

## Interaction and Accessibility

- Preserve semantic headings, navigation landmarks, descriptive links, and the existing accessible SVG chart label.
- Provide visible keyboard focus styles.
- Meet comfortable touch target sizing for primary actions on mobile.
- Respect `prefers-reduced-motion`; any reveal or hover motion must be nonessential.
- Do not add a hamburger menu when three compact navigation links can remain usable; if space is insufficient, hide only the in-page links while keeping the labeled GitHub action.

## Technical Constraints

- Keep `web/landing/index.html`, `web/landing/main.tsx`, and `web/vite.landing.config.ts` as the separate GitHub Pages entry and build configuration.
- Do not add runtime dependencies or external font requests.
- Keep the landing page functional as static files with no API dependency.
- Keep application routes and the normal `npm run build` output unchanged.
- Use the existing React, TypeScript, Vite, Vitest, Testing Library, and Lucide stack.

## Verification

- Add a focused component test for the revised content hierarchy and verified capability facts before implementation.
- Run the focused test first and observe it fail, then implement until it passes.
- Run the complete Vitest suite, `npm run build`, and `npm run build:landing`.
- Inspect the live landing page in the collaborative browser at 320, 390, 768, 1024, 1280, and 1440 px.
- At each viewport, verify `scrollWidth === clientWidth`, no content element extends outside the document, the hero hierarchy remains readable, and section height changes are continuous rather than caused by rigid minimum dimensions.

## Out of Scope

- Changing the authenticated application UI.
- Adding real customer logos, testimonials, or performance metrics without source data.
- Changing repository visibility, GitHub Pages settings, or deployment credentials.
