### Added

- A pooled agent account can take its credential from a tenant secret instead
  of holding it inline: `credential_secret_ref` names the secret by its short
  name (e.g. `agents/claude/claude1/oauth_token`), and `credential_secret_file`
  materializes it as a file (e.g. `.codex/auth.json` for the codex reviewer).
  The secret is read in the ACCOUNT's tenant on every spawn and release, so
  writing it rotates the very next spawn with no account write — on a remote
  agent daemon too, because a referenced account's release is marked
  `single_use` and the daemon re-releases it per spawn instead of caching it.
  Exactly one of an inline credential and a reference is accepted; a reference
  is static-token only and must resolve when it is written. A secret that later
  goes missing parks the work uncharged and puts the account in `error` with a
  `last_error` naming the reference. On a remote agent daemon the release
  refusal carries `fault_class: credential_not_found` in its error body, which
  the daemon decodes whole (up to 64 KiB, so a long message cannot cut the
  class off) and rebuilds into the missing-credential fault. Re-applying the
  reference (`account update --credential-secret-ref`) returns the account to
  service. `update --credential-secret-ref` also switches an inline account
  over and destroys its inline envelope; if that destroy fails, retrying the
  same update finishes it, since every update of a referenced account removes
  any leftover envelope. Wired on REST, gRPC, MCP, QUIC, Unix, embedded, the Go
  and TypeScript SDKs, and `quicbaas agent account create|update
  --credential-secret-ref [--credential-secret-file]` (go-quicbaas#1023).
- `quicbaas secrets put --from-stdin | --from-file PATH` stores a secret's raw
  bytes verbatim, with no `KEY=VALUE` JSON envelope; from an interactive
  terminal `--from-stdin` reads one line with echo disabled. Interactivity is
  detected by the terminal driver, so `--from-stdin </dev/null` is refused as
  an empty value rather than prompting. The value never
  appears on the command line or in the output (go-quicbaas#143).
- Each resolution carries the delivery grant its credential was served under,
  so two concurrent spawns of a single-use account each report their own grant
  and every release stays tracked by session teardown.
- An account update is a conditional write: it lands only if the account is
  unchanged since it was read, and otherwise re-reads and re-applies. A rename
  overtaken by a switch to a secret reference can no longer save a stale copy
  that leaves the account with no credential source. Every account record
  write (update, revoke, and every usage, refresh, status, cooldown and expiry
  stamp) now commits with the KV's compare-and-swap, so a writer in another
  store instance or daemon can no longer be overwritten; a lost update is a
  `409` / gRPC `Aborted`. An inline rotation overtaken by a switch to a secret
  reference is refused (`AccountCredentialSourceChangedError`) and removes the
  envelope it wrote instead of re-creating one on a referenced account; an
  automatic OAuth renewal overtaken the same way does the same.
- Automatic account stamps no longer undo an operator: a renewal outcome or a
  failed secret read never replaces `revoked`, and a failed-read `error` is
  stamped only if the account still reads the reference that read attempted,
  as last applied — so re-pointing or re-applying a reference while an older
  read fails leaves the working account in service.
- A failed `credential_secret_ref` read reports only a sanitized refusal (the
  reference and a failure class) on REST, gRPC and MCP; the secrets service's
  own error, and any diagnostics it carries, never leave the process.
