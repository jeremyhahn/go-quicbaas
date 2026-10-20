### Fixed

- **gRPC SessionService checks the session's owner, as REST does
  (go-quicbaas#1624).** `GetSession`, `RevokeSession` and
  `RevokeOtherSessions` now refuse, with `PermissionDenied`, a session that
  belongs to another tenant or to another user in the caller's tenant. The
  check runs before anything is revoked. `ListSessions` and
  `RevokeAllSessions` accept only the caller's own user ID, and an empty
  `user_id` means the caller.
- **Anonymous federated login works over gRPC (go-quicbaas#1624).**
  `BeginFederatedLoginRequest` gains `provider_id`, and the public
  `BeginFederatedLogin` resolves it on the server. The SDK no longer calls
  the authorized `GetProvider` first, which an anonymous caller was refused.
  A request must name exactly one of `provider_name` and `provider_id`.
- **gRPC user approval takes REST's grant (go-quicbaas#1624).** The new
  `IAMService.ApproveUser` RPC, which needs `users:enable`, moves a pending
  user to active, as `POST /api/v1/iam/users/{user_id}/approve` does. The
  SDK's `Approve` calls it instead of the unmapped `UpdateUser`.
