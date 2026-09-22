### Changed — a real design system for the web UI, and status pills that don't wash out

- **A single, contrast-tested palette drives every colour token.** The old
  theme blocks picked lightness/chroma by eye and it showed: solid
  `badge-success` measured 3.4:1 white-on-green in light theme, the dark
  theme never declared `*-content` colours for its semantic tokens at all
  (daisyUI's auto-derived contrast was the only thing standing between a
  badge and invisible text), and the `/25`–`/60` muted-text opacity
  modifiers used across ~40 view files true-blended to well under 4.5:1 in
  BOTH themes. `cmd/quicbaas/frontend/src/lib/theme/tokens.ts` is now the
  palette of record — every base surface, every semantic solid fill, and
  every StatusBadge tone, as hex, for both themes — and
  `test/themeContrast.test.ts` grades all 55 foreground/background pairs
  against WCAG AA on every run. `src/app.css` carries the same values
  restated as `oklch()` (daisyUI needs the CSS-variable form; a comment at
  the top of each file points at the other). The four `text-base-content/NN`
  opacity tiers now resolve to two dedicated, AA-passing tokens via a CSS
  override, so every existing call site is fixed without a 40-file edit.
- **StatusBadge renders a soft tinted pill, not a saturated fill.** A pale
  (light theme) or deep (dark theme) tint of the status's tone sits behind
  the text; the text is the SAME tone's deep or vivid colour, never white
  stamped over a colour that may or may not have enough contrast to carry
  it. Proper padding (`px-2.5 py-1`, `rounded-full`) replaces the cramped
  `badge-sm` that produced the "no padding" complaint on the Jobs State
  column. One tone table, `STATUS_TONES`, covers job states, task statuses,
  session states and agent statuses; `AgentFleet`'s hand-rolled
  `badge-xs {success|error|ghost}` switch is gone in favour of the same
  component every other list uses.
- **FilterBar stops chips from one facet bleeding into the next.** Each
  field now renders inside its own bordered row instead of every field's
  chips sharing one flex-wrap run — wrapping happens within a field, never
  across two. A field whose vocabulary exceeds 8 values (the job lifecycle
  is 13 states) shows the first 8 plus a "+N more" disclosure; a value the
  operator has actually selected is never among the ones hidden by it. The
  "Filtered by" row above the groups is now a calm tinted chip in the
  primary tone instead of a solid `badge-primary` block. The URL grammar,
  the loader contract, and every existing `data-testid` are unchanged —
  `FilterBar.test.ts`, `DataTable.test.ts`, `JobsView.test.ts`,
  `TasksView.test.ts` and `SessionList.test.ts` pass without modification.
- Job states gained explicit tones instead of falling through to `info`:
  `validating`/`reviewing` read as active work (same tone as
  `executing`/`assigning`); `initializing`/`decomposing`/`scheduling` and
  `completing`/`cleanup` read as informational setup/wind-down phases.
