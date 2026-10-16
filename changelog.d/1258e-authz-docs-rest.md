### Changed

- **Authorization docs now cover only what go-quicbaas adds (go-quicbaas#1258).**
  `docs/authorization/architecture.md`, `engineering.md` and `user.md` link to
  go-qrdb for the RBAC authorizer, the ABAC decorator and the policy schema.
  They document only the per-tenant ABAC policy store, its sync into go-qrdb
  and the CLI tasks. Content describing code that does not exist is removed.
- **The KMS backend-selection section is reduced to the resolver as built
  (go-quicbaas#1258).** `docs/kms/engineering.md` now describes the
  `BackendResolver` surface, the hardware probe, the build tags, its callers
  and the known gaps, instead of the implementation plan.
- **A broken SDLC anchor in `docs/software-factory/requirements.md` is fixed
  (go-quicbaas#1258).**
