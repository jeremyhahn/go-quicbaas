### Fixed

- **REST policy create answers 409 for a duplicate name (go-quicbaas#1417).**
  Creating a policy whose name already exists in the tenant returned HTTP 500.
  The REST error mapping now answers `iam.PolicyExistsError`, wrapped or
  not, with 409 Conflict and the `conflict` error body.
