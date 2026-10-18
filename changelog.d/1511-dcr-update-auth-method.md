### Fixed

- **Dynamic client update carries `token_endpoint_auth_method`
  (go-quicbaas#1511).** `PUT /oauth2/register/{client_id}` (RFC 7592) now
  applies the requested method and reports the stored one. It is checked by
  the same validator register uses; a method it refuses is
  `invalid_client_metadata` (400) and leaves the client unchanged.
- **Dynamic client register and update answer 500 for server failures
  (go-quicbaas#1511).** Only a metadata refusal, now the typed
  `iam.AppMetadataError`, is `invalid_client_metadata` (400). Any other error,
  such as a storage failure, is `server_error` (500) with a generic
  description that carries no internal detail.
