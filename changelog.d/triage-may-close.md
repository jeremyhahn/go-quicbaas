### Added

- **Triage can close an issue whose work is already on the default branch,
  when the project allows it.** `ProjectBinding.TriageMayClose` is off by
  default and the charter still tells the agent not to ask unless the project
  has enabled it — a label is reversible and carries no social weight, and
  closing somebody's issue does. Closing requires BOTH the binding's consent
  and an explicit `SDLC-TRIAGE-CLOSE: already-done` marker on the verdict, so
  enabling the feature cannot retroactively close what an earlier pass judged,
  and a not-actionable that named no reason is never closed. Only that one
  reason closes: a model inventing its own justification cannot act on it. The
  evidence comment is posted first, so a crash between the two leaves an open
  issue carrying the reasoning rather than a closed one saying nothing.
