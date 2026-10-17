### Fixed

- **`/oauth2/revoke` authenticates the client (go-quicbaas#1447).** The REST
  handler never passed the client credentials to the provider, so every
  revocation answered `400 invalid_client`. The endpoint now reads them as the
  token endpoint does (HTTP Basic, else `client_id`/`client_secret` from the
  form body, never the query string) and answers `401 invalid_client` on a
  failed authentication, with the Basic challenge when Basic was used.
- **A client may revoke only its own tokens (RFC 7009 §2.1).** A token issued
  to another client is refused with `400 unauthorized_client`
  (`iam.ErrRevokeTokenNotOwned`) and stays active. The owner is the `client_id`
  claim of the verified access token, or the client recorded with a refresh
  token.
- **Revocation answers `200` (RFC 7009 §2.2)**, not `204`, including for a
  token that is unknown, expired or already revoked.
