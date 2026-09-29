# SSH — CLI

> **Status: NORMATIVE** · Last verified: 2026-09-24
> Every verb and flag, with runnable examples. Feature folder: `docs/ssh/`.

Server-side SSH key store backed by [go-xkms](https://github.com/jeremyhahn/go-xkms).
Private key material is mlocked into a memguard `Secret` and never
written to the operator's filesystem. Operators consume the keys
indirectly: `quicbaas fleet deploy --ssh-key-name <name>` looks up
the key in the store and uses it to dial the remote host without
ever materialising the PEM bytes outside the server process.

## Transport

Every subcommand goes through `quicbaas.SDK().Security().SSH()`, so SSH uses
the same global `--server` / `--transport` / `--tls-*` flags as every other
`quicbaas` verb and is subject to the same authentication and authorization.
All six transports (rest, grpc, quic, mcp, unix, embedded) serve the full SSH
surface, so a script written against one works unchanged against the others.

> The CLI no longer builds a go-xkms client of its own, and the `XKMSD_URL` /
> `XKMSD_TOKEN` environment variables and the `~/.quicbaas/ssh-store` local
> file store are gone. To reach a remote store, point `--server` at the
> quicbaasd that owns it.

> Keys live in quicbaasd's barrier-sealed store, never on disk (see
> [architecture](architecture.md#where-the-keys-live)). The CLI never reads
> `~/.quicbaas/ssh-store`. A copy left there by an old CLI is dead plaintext:
> confirm the daemon holds the key you deploy with
> (`quicbaas ssh keys get --scope user --owner $USER --name <name>`), then
> destroy the old copy:
> `find ~/.quicbaas/ssh-store -type f -exec shred -u {} + && rm -r ~/.quicbaas/ssh-store`.

## Subcommands

| Command | Description |
|---------|-------------|
| `keys generate` | Create a new key inside the xkms store |
| `keys import` | Import an existing PEM private key (operator action — needs `--from-file`) |
| `keys export-public` | Print the public half so it can be added to `authorized_keys` |
| `keys list` | List stored keys filtered by scope |
| `keys delete` | Permanently remove a key from the store |
| `known-hosts add` | Pin a remote host's public key (used by deploys) |
| `known-hosts list` | Show pinned hosts |
| `known-hosts remove` | Unpin a host |
| `known-hosts import-file` | Bulk-import an existing `~/.ssh/known_hosts` |
| `authorized add` | Store an `authorized_keys` line under a name |
| `authorized list` | List stored `authorized_keys` entries |
| `authorized remove` | Delete a stored `authorized_keys` entry |

## Scope model

Every key has a `scope` and an `owner`:

| Scope | Owner default | Meaning |
|-------|---------------|---------|
| `user` | `$USER` | Personal key, visible only to that operator |
| `tenant` | active tenant | Shared by every operator in the tenant |
| `service` | `default` | Service account credential, used by automation |

The deploy command resolves keys via `(scope, owner, name)`. If
`--ssh-key-owner` is omitted it falls back to `$USER` for `user`,
the active tenant for `tenant`, and `default` for `service`.

## quicbaas ssh keys generate

```bash
quicbaas ssh keys generate \
  --name fleet-deploy \
  --type ed25519 \
  --scope user
```

| Flag | Description |
|------|-------------|
| `--name` | Required. Identifier within the chosen `(scope, owner)`. |
| `--type` | `ed25519` (default) or `rsa-4096`. ECDSA is rejected. |
| `--scope` | `user` \| `tenant` \| `service`. Defaults to `user`. |
| `--owner` | Override owner. Defaults per scope (see above). |
| `--comment` | Free-text comment that ends up in the public key. |

Output prints the public key — copy it into `~/.ssh/authorized_keys`
on every host you intend to deploy to.

## quicbaas ssh keys import

```bash
quicbaas ssh keys import \
  --from-file ~/.ssh/id_ed25519 \
  --name laptop-key \
  --scope user
```

Reads the PEM private key from the file, hands it directly into
`memguard.Secret`, and stores it. The file on disk is **not** wiped
automatically — that's the operator's responsibility.

> Importing an OpenSSH passphrase-encrypted key is supported; the CLI
> prompts for the passphrase via `golang.org/x/term.ReadPassword` so
> the secret is never copied through a Go `string`. The decrypted
> private bytes flow through a `memguard.Buffer` straight into the
> store.

## quicbaas ssh keys export-public

```bash
quicbaas ssh keys export-public --name fleet-deploy --scope user --owner $USER
```

Prints the OpenSSH-format public key. The private half **never**
leaves the server.

## quicbaas ssh keys list

```bash
quicbaas ssh keys list --scope user
```

```
NAME           SCOPE  OWNER   TYPE     FINGERPRINT (SHA256)
fleet-deploy   user   deploy  ed25519  ZF8u2vF+...
laptop-key     user   deploy  ed25519  Pa9g3xR-...
```

## quicbaas ssh known-hosts

Pin remote host keys ahead of a deploy so the SSH transport rejects
key changes.

```bash
quicbaas ssh known-hosts add \
  --host gpu-node-01 \
  --key "ssh-ed25519 AAAAC3Nza..." \
  --scope user
```

```bash
quicbaas ssh known-hosts list
```

```
HOST         SCOPE  OWNER   TYPE     FINGERPRINT
gpu-node-01  user   deploy  ed25519  SHA256:abc...
```

## quicbaas ssh authorized

Store `authorized_keys` entries server-side so a fleet's inbound key policy
lives next to the keys themselves rather than in per-host files.

```bash
quicbaas ssh authorized add "$(quicbaas ssh keys export --name fleet-deploy)" \
  --name fleet-deploy --scope user
```

```bash
quicbaas ssh authorized list --scope user
```

```bash
quicbaas ssh authorized remove --name fleet-deploy --scope user
```

| Flag | Description |
|------|-------------|
| `--name` | Entry name, unique within `(scope, owner)`. Required for add/remove. |
| `--scope` | `user` \| `tenant` \| `service`. Defaults to `user`. |
| `--owner` | Override owner. Defaults to `$USER`. |

## See also

- [Fleet quick start](../software-factory/user.md)
- [Agent deploy reference](../software-factory/cli.md)
- [SDK security surface — SSH](engineering.md#ssh--sshservice)

## `quicbaas ssh agent` — use a KMS-held key from git and ssh

`quicbaas ssh agent` serves a local ssh-agent socket for one stored key. The
agent holds only the public half; every signature is made by quicbaasd, so
the private key never leaves the KMS. It prints `SSH_AUTH_SOCK=<path>` once
listening (and writes it to `--env-file`, mode 0600), then runs until
interrupted, removing the socket on exit.

```sh
quicbaas ssh agent --scope service --owner agent-go-codec \
  --name agent-go-codec-signing --env-file /run/user/$UID/codec-agent.env &
. /run/user/$UID/codec-agent.env; export SSH_AUTH_SOCK

# Push over SSH and sign commits with the same KMS key:
PUB=$(quicbaas ssh keys export --scope service --owner agent-go-codec --name agent-go-codec-signing)
git -c gpg.format=ssh -c user.signingkey="key::$PUB" commit -S -m "..."
git push
```

`--socket-dir` names the directory for the socket; it must not exist yet
(the agent creates it 0700 and never adopts a directory it did not create).
The default is a private temporary directory.
