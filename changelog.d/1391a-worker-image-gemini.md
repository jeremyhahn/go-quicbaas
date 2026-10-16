### Added

- **The worker image installs the Gemini CLI** (`@google/gemini-cli`, pinned
  by the Makefile's `GEMINI_CLI_VERSION`, 0.64.0) beside Claude Code and
  Codex, for operators to use inside the container; nothing in the factory
  drives it. The image carries `io.quicbaas.worker.gemini.version`, the
  worker image probe checks `gemini --version` against `-gemini`, and the
  refresh installs npm's latest Gemini and refuses a rebuild that would
  downgrade it. Gemini is not in the content tag, so it never makes an image
  stale by itself (#1391).
