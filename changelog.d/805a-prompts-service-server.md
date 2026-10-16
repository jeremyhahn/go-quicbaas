### Added

- **Prompt sets are served over REST, gRPC and MCP (go-quicbaas#805).**
  `service.PromptService` carries eight verbs over a tenant's prompt
  overrides: List, Get (by version too), Effective (with the resolution
  chain and its origin), Diff against the parent scope, History, Put
  (compare-and-swap on `expected_version`, `note` required), Rollback and
  Reset. Each verb is addressed by tenant, `scope` (`tenant`, `project` or
  `system`, which is read-only) and project.
- REST serves them under `/api/v1/prompts/{set}[/{name}]`. gRPC serves them as
  `quicbaas.prompt.v1.PromptService`, which Unix also serves. MCP serves them
  as the `prompt_*` tools. Reads need `prompts:read`, History needs
  `prompts:history`, and changes need `prompts:write`.
- Refusals map the same on every transport. A missing marker (named in
  the body), an unknown set or name, or a write at `system` is 400 /
  InvalidArgument. Not found is 404 / NotFound. A stale version is 409 /
  Aborted. No prompt store is 503 / Unavailable. Naming another tenant is
  403 / PermissionDenied.
- **The Go SDK and the CLI carry the prompt verbs (go-quicbaas#805).**
  `Client.Prompts()` returns `quicbaas.PromptsClient` with the same eight
  verbs, served by every transport: REST, gRPC, MCP and embedded have their
  own handlers, QUIC sends the REST routes over HTTP/3 and Unix delegates to
  gRPC. Each refusal keeps its class in the SDK (`IsInvalidInput`,
  `IsNotFound`, `IsConflict`, `IsUnavailable`); a transport without the
  service is refused with `PromptTransportUnsupportedError`.
- `quicbaas prompts list|get|effective|diff|history|put|rollback|reset` edits
  them from the command line, with `--project`, `--scope`,
  `--output table|json|yaml`, `--raw`, `-f FILE|-`, `--note`,
  `--expected-version`, `--version` and `--yes`. `rollback` and `reset` ask
  before they write unless `--yes` is given.
- Every gRPC service on the unified listener now reads the caller qrdb
  authenticated as its Subject, as REST and MCP already did, so prompt
  writes there name their author instead of being refused.
