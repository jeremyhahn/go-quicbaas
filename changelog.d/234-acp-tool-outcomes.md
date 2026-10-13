### Added

- **The sdlc turn record says which tool calls failed (go-quicbaas#234).**
  `AgentTurn` now has `tool_failures`, the count of `tool_result` blocks the
  backend marked as errors. It also has `failed_tools`, the names of those
  calls, matched to their `tool_use` blocks by tool-use id (`unknown` when the
  turn has no matching call) and capped at eight. `failed_tools_cut` counts
  the names the cap dropped. These fields are read only from blocks, never from
  the reply text, so a reply that says a tool failed records no failure. A turn
  with no `tool_result` block has no `tool_failures` field. The
  `sdlc:agent_turn` event header shows the same fields.
