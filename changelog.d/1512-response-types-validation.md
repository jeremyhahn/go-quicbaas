### Fixed

- **App register and update refuse response types the server does not
  serve** (#1512). A `response_types` value outside `response_types_supported`,
  or `code` without the `authorization_code` grant (RFC 7591 §2.1), fails with
  `iam.ErrInvalidResponseType` (invalid input) on every transport, and nothing
  is stored; Dynamic Client Registration answers 400 `invalid_client_metadata`.
  Discovery and validation read one list, `iam.SupportedResponseTypes()`.
  The DCR request check reads that same list; its own copy, which accepted
  `token` and `id_token`, is gone.
