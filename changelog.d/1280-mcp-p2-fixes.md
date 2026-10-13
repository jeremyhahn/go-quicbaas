### Fixed

- **The MCP `StartJob` tool now reports the right start time
  (go-quicbaas#1280).** It used to read the nanosecond timestamp as seconds,
  which gave a date far in the future. The time is now rendered in UTC to the
  nanosecond.
- **A stream-json turn with a tool block that cannot be decoded now reports
  an unknown tool census (go-quicbaas#1280).** Before, it reported a count
  that left that call out.
- **Go SDK content blocks now keep their tool metadata (go-quicbaas#1280).**
  `PromptContentBlock` and `InferenceContentBlock` carry `tool_name`,
  `tool_use_id`, `is_error` and `bytes` across the embedded, gRPC, JSON and
  MCP transports.
- **The TypeScript SDK's unpaged MCP listings now return every item
  (go-quicbaas#1280).** `listJobs`, `listSessions`, `listFleetNodes` and
  `listWorkflows` follow `next_offset` to the last page. `listWorkflowsPage`
  now passes `project_id` and `mode` to the tool, and MCP sessions keep their
  `agent_id`.
