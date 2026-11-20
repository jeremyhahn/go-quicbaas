### Added

- **The SDK root package builds the DEK and secrets clients and names every
  type they use (go-quicbaas#2609).** `quicbaas.NewDEKClient(t)` and
  `quicbaas.NewSecretsClient(t)` build the DEK-management and secrets clients
  over a consumer's own `quicbaas.Transport`, and the root package now
  aliases every DEK and secrets request and response type it did not before
  (`SetDefaultDEKResponse`, `SetDEKSafetyRequest`, `StartReencryptionRequest`,
  `StartReencryptionResponse`, `GetReencryptionProgressRequest`,
  `ReencryptionProgressResponse`, `ReencryptionProgress`,
  `EnsureSecretRequest`, `EnsureSecretResponse` and `SecretStatus` with its
  three values). A consumer reaches every DEK and secrets operation through
  `sdk/go` alone, without generated `api/proto` types. Before, it had to
  import `sdk/go/client/dek` or `sdk/go/client/secrets`, or the generated
  `api/proto` clients.
