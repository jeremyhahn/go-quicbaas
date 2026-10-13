### Fixed

- **A malformed IAM create or update is refused as malformed, whatever the
  server's state (go-quicbaas#335).** Creating a pending user with a malformed
  email on a server with no invite store answered `Unavailable` on gRPC and
  500 on REST, because the invite store was checked before the request.
  `pkg/iam` now validates every field before any lookup or dependency check:
  user create before the invite store, role references and tenant quota;
  user update before the user and its named roles are looked up; role update
  before the role is looked up. A malformed field therefore answers 400 /
  `InvalidArgument` on every transport. A well-formed pending create with no
  invite store is still refused, and REST now reports it the way gRPC does:
  503 Service Unavailable (it was 500), because REST no longer resolves the
  store itself but leaves that refusal to the shared `CreateInvitedUser`.
