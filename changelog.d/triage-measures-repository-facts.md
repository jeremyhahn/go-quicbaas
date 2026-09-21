### Added

- **Triage measures the repository facts itself.** The file paths and
  backticked identifiers an issue names are probed with `git ls-files` and
  `git grep` in the session's checkout before the model is prompted, and the
  answers go into the prompt as a "Repository facts" section measured at the
  head being judged. A frontier model would have run those commands; the NPU
  model never did — `tool_uses=0` on every triage turn of 2026-09-21 while it
  wrote "I have examined the code" — so its already-done verdicts were
  ungrounded by construction. The deterministic half of the check is now done
  in Go for every rung; the judgement stays with the model.
