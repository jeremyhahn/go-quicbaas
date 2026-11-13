### Removed

- **The `canned` workflow mode (go-quicbaas#622).** It was declared and
  documented as "work items are predefined" but never built: the lead
  decomposed the task anyway. A workflow with `mode: canned` is now refused
  at validation, and the error names the modes that exist (`constrained`,
  `autonomous`). The Go SDK's `orchestrator.WorkflowModeCanned` constant, the
  TypeScript SDK's `'canned'` `WorkflowMode` value and the workflow wizard's
  Canned option are gone. Move a canned workflow to `constrained`, which
  behaves as canned did.
