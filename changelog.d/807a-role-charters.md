### Changed — BREAKING

- **Workflow roles start with a charter from the prompts store; `system_prompt`
  is retired (go-quicbaas#807, pre-release).** The lead, worker and reviewer
  sessions and the `agent` validator's prompt now carry `charter_lead`,
  `charter_worker`, `charter_reviewer` and `charter_validator`, rendered from
  the job's resolved `workflow` prompt set, so a tenant or project overrides
  them with `quicbaas prompts`. A charter that cannot be rendered is a
  `CharterRenderError` (`ErrCharterRender`) and the session is not spawned.
  `system_prompt` is removed from workflow role definitions on every
  transport (proto field 6 is reserved), and the workflow loader refuses a
  file that sets it with a `RetiredFieldError` (`ErrWorkflowRetiredField`).
  Use `agents[].prompt_append` to add to a role's charter.
