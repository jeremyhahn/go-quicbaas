### Fixed

- Pooled provider accounts: the codex backend on the control plane and on an embedded fleet node now draws a pooled account, as the claude and opencode backends do, instead of refusing every pooled codex spawn. A credential release refused by the spend cap answers 409 over REST and a matching gRPC code instead of 500 or Internal, and a revoked delivery grant over gRPC answers FailedPrecondition. `quicbaas orchestrator rotate-account` takes `--available-at`, and the spend-policy help no longer says tenant limits are unenforced (#1678).
