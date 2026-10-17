### Fixed

- **The embedded transport serves IAM policy Update, Validate and Test
  (#1449).** `quicbaas.IAM.Policy/Update` merges the set fields into the
  stored policy through `PolicyEngine.UpdatePolicy` (`spec_yaml` replaces the
  rules) and refuses fields the engine cannot store with
  `UnsupportedPolicyFieldError`. `Validate` parses the document with Create's
  strict decoder, checks its name and reports failures as `Valid=false` with
  the reasons in `Errors`, as gRPC does. A missing `spec_yaml` is an
  `INVALID_INPUT` error. `Test` evaluates each case against the tenant's
  stored policies, as gRPC does, and reports pass or fail per case.
