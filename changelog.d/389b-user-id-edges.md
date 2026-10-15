### Changed

- **Admin, impersonation, GDPR and token-claim user ids are uint64
  (go-quicbaas#389).** `AdminAssignment.UserID`, `AdminCheckRequest.UserID`,
  `ImpersonationSession.AdminUserID` / `TargetUserID`, the admin and
  impersonation option structs, GDPR `RequestedBy`, `interfaces.Subject.UserID`
  and the platform token `uid` claim (`platformtoken.Claims.UserID`,
  `AuthorizedContext.UserID`) now hold qrdb's uint64 Snowflake. In JSON each
  is a decimal string, so an id above 2^53 keeps its precision.
- `IAMServiceAdapter` and the `pkg/service` admin and GDPR requests take the
  uint64 id. gRPC parses the proto string once in its handler; the embedded
  transport parses once at the request edge.
- REST parses `target_user_id`, `requested_by` and the `user_id` /
  `admin_user_id` query filters once, answering `400` for a value that is
  not a non-zero decimal uint64. The user-assignment and impersonation list
  responses carry `user_id` as a decimal string. MCP parses `user_id`,
  `target_user_id`, `admin_user_id` and `requested_by`, and its
  `UserIDArgError` now names the argument it refused.
- Still strings: OIDC subjects and the refresh token's `Subject`, which
  name a user or a service; audit actor ids, since an actor may be a
  service; and `BatchError.ID` and `UserImportItem.ID`, which are not
  Snowflakes.
