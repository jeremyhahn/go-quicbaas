### Changed

- **The TypeScript SDK claims a Shamir share in two steps.** `ceremony.claimShare`
  is gone; `claimShareBegin({username})` returns `{nonce_hex, expires_at}` and
  `claimShareComplete({username, nonce_hex, signature})` (base64 signature)
  returns `{share_index, share_json}`. REST calls
  `POST /api/v1/init/officer-share/begin` and `/complete`; MCP calls
  `qrdb_ceremony_begin_claim_share` and `qrdb_ceremony_complete_claim_share`,
  decoding the tool's base64 share. The server binds both calls to the caller's
  mTLS certificate thumbprint. QUIC and gRPC-Web refuse both verbs with their
  typed not-implemented errors. The svelte-security `ClaimShareRequest` is now
  `{username, challenge, signature_b64}`, and the frontend's Claim Share form
  takes a challenge and a signature as the Claim Certificate form does.
