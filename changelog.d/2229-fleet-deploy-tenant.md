### Fixed

- **`quicbaas fleet deploy` binds the deploy tenant (go-quicbaas#2229).** The
  CLI command, its `--preflight-only` path and the desktop binding now bind
  the spec's tenant, or the system tenant when the spec names none, before
  calling the in-process deploy service. Before, every deploy failed with
  "no tenant is bound to the request".
