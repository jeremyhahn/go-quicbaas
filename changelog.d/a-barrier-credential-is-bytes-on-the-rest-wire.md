### Fixed

- **`barrier unseal --transport rest` can unseal a node again.** go-qrdb
  v0.19.0-alpha (adopted in #963) retyped every barrier credential from
  `string` to wipeable `[]byte`, which makes `secret` a BASE64 string in the
  JSON body its REST transport sends. This repository's REST handlers kept
  decoding those fields as `string`, so the barrier was asked to decrypt with
  the base64 TEXT of the passphrase. Measured on the burn-in node
  (2026-09-22): a v0.11.0-alpha CLI answered
  `quicraft/seal: unseal failed (strategy=passphrase): quicraft/seal:
  decryption failed` against a node the pre-#963 CLI unsealed with the same
  passphrase. Every credential-bearing barrier request on the REST surface —
  `barrier initialize` / `unseal` / `shamir/initialize` /
  `enable-encryption`, and the per-tenant `barrier/init` /
  `barrier/enable-encryption` — now decodes the credential as `[]byte`,
  matching go-qrdb field for field (#988).

### Changed

- **The barrier REST request shapes are now type ALIASES of the go-qrdb
  transport types, not local copies of them.** The stack is pre-v1 with
  exactly one wire format, and `go-qrdb/sdk/go/transport` is where it is
  written down; a mirror of those structs is a second copy of one contract
  that drifts in silence, which is how the defect above shipped. A field
  renamed or retyped upstream is now a compile error here.
- **A barrier credential is base64 on the REST wire, on every client.** A
  request whose credential is not well-formed base64 is refused with 400 and
  never reaches the barrier. That is the whole of the protection and it is
  stated narrowly on purpose: a raw credential that happens to be valid base64
  — `password` is one — decodes into DIFFERENT bytes and is used as the
  credential with no complaint, so a client must encode rather than rely on
  being told off. The TypeScript SDK's `secret` / `passphrase` fields are
  documented and tested as base64 accordingly, matching the byte-slice
  convention the rest of that SDK already follows. Share fields (`share`,
  `shares`, `keys`) are unchanged: a Shamir share is already a printable
  encoding and go-qrdb keeps it a `string`.

### Security

- **On every return path, not only the successful one, the credential slice a
  barrier request field holds is wiped.** `encoding/json` populates earlier fields before it reports
  a later field's type error, so a body carrying a well-formed `secret` and a
  malformed `threshold` left the plaintext on the heap — as did every
  validation branch that answered 400 between the decode and the service call.
  The per-tenant `barrier/unseal` route never wiped its `key` at all.
  `defer secure.Wipe(req.Secret)` could not cover either, because `defer`
  evaluates its arguments immediately and captures the nil slice that exists
  before the decode runs. The cleanup is now registered before the decode and
  reads the field through a pointer, so it zeroes whatever the decode actually
  produced. The guarantee is stated narrowly on purpose: it covers that slice
  and every alias of it, and not an allocation `encoding/json` made and then
  dropped — a duplicate JSON key that re-assigns the field, or the decoder's
  scratch buffer on a malformed value. Those are unreferenced and collected but
  never zeroized, and nothing in this package can zeroize them; go-qrdb states
  the same limit for the encode side.
