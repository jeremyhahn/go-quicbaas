### Added

- **`reviewer.reasoning_effort` on the project binding.** For a codex
  reviewer it becomes `-c model_reasoning_effort=…` (`minimal`, `low`,
  `medium`, `high`, `xhigh`), validated at bind time; empty keeps the
  backend's default. A fleet generating reviews by the dozen while its
  pipelines are brought up runs at medium and turns it up per project later.
