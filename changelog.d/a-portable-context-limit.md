### Added

- **A portable context limit for served models.** Every model has a prompt
  limit and almost no server will tell you what it is: measured against one
  OpenAI-compatible server, the model endpoints return `id`, `object`,
  `created` and `owned_by`, and the metadata endpoint answers 500. Others
  expose *something* — a `max_model_len`, an `n_ctx`, a `max_input_length` —
  but they do not agree on where or what it is called, so a client that reads
  any one of them works against one server.

  `ContextLimit` establishes the limit by a ladder instead, every rung
  optional: **declared** (`AgentProfile.ContextTokens`, the only rung that can
  be right before the first request), **learned** (a refusal narrows it, which
  costs nothing until something overruns), and **unbounded** (send it and let
  the server decide). The learned bound wins when it is tighter, because the
  server is the authority on its own configuration.

  Deliberately absent: model geometry, KV-cache arithmetic and `config.json`
  parsing — layer and head counts are a transformer-family convention, not a
  universal one — and per-vendor metadata probes, which would each be a guess
  about one server wearing the costume of a general mechanism.

### Fixed

- **A prompt that exceeds the model's context is reported as itself.**
  `PromptTooLongError` names the size sent and the ceiling in force, unwraps to
  its own sentinel so a caller can tell it from a transient fault, and is
  raised locally — without a round trip — once the ceiling is known. It
  previously surfaced as an opaque server error wrapped in whatever the agent
  said next; on one fleet every triage turn failed that way for a day before
  anyone read the raw response body.
