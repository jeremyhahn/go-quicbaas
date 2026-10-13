### Fixed

- **A turn's tool use is now in its content (go-quicbaas#799).** Two backends
  used to drop every block that was not text: the stream-json CLI and codex
  `exec --json`. Those backends now keep `tool_use`, `tool_result` and
  `thinking` blocks. Each kept block carries its tool name, tool-use id, error
  flag and byte size, but never its payload. A block that cannot be decoded
  becomes an `unreadable` block, and the text around it is kept. The new
  `ContentBlock` fields cross the remote_acp and orchestrator gRPC wires.

### Changed

- **More backends report a tool census.** codex `exec --json` now reports
  `PromptResponse.ToolUse`, one call per tool item. If any completed item
  cannot be decoded, its census is unknown (nil), not a count. Ollama and API inference
  report a zero census, because their requests offer the model no tools. The
  sdlc turn census prints `tool_uses=unreported` when the backend reports no
  census and the content holds no `tool_use` block. A turn whose tool use was
  not reported is no longer classified as a non-answer.
