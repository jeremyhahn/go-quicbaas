### Changed

- **IAM user ids are qrdb's uint64 Snowflake (go-quicbaas#389).**
  `iam.User.ID` is now a `uint64`, the id qrdb assigns, and zero means "not
  created yet". The iam user-storage interface, `MemoryStorage` and the
  qrdb adapter take and return it, and so do the Snowflake user-id fields in
  `pkg/iam` and `pkg/service`. `ParseUserID`, `FormatUserID` and
  `User.QRDBID` are removed. Both stores refuse id `0` in `GetUser`,
  `MarkUserDeleting` and `DeleteUser` with `ErrInvalidUserID`.
- The JSON encoding of these ids is a decimal string (`"id":"…"`), so an
  id above 2^53 keeps its precision in a JavaScript client. YAML carries the
  plain integer.
- REST, gRPC, MCP and the embedded transport still take a user id as a
  string. They parse it where it enters, and refuse a value that is not a
  non-zero decimal uint64 with a typed error: `400` on REST,
  `InvalidArgument` on gRPC, an invalid-input `UserIDArgError` on MCP and
  `InvalidUserIDError` on the embedded transport. OIDC subjects, external
  IdP ids and handles stay strings.
