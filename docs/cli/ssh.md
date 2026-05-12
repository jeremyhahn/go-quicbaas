# quicbaas ssh

Server-side SSH key store backed by [go-xkms](https://github.com/jeremyhahn/go-xkms).
Private key material is mlocked into a memguard `Secret` and never
written to the operator's filesystem. Operators consume the keys
indirectly: `quicbaas agent deploy --ssh-key-name <name>` looks up
the key in the store and uses it to dial the remote host without
ever materialising the PEM bytes outside the server process.

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
fleet-deploy   user   jhahn   ed25519  ZF8u2vF+...
laptop-key     user   jhahn   ed25519  Pa9g3xR-...
```

## quicbaas ssh known-hosts

Pin remote host keys ahead of a deploy so the SSH transport rejects
key changes.

```bash
quicbaas ssh known-hosts add \
  --host gpu-host \
  --fingerprint "SHA256:abc..." \
  --scope user
```

```bash
quicbaas ssh known-hosts list
```

```
HOST       SCOPE  OWNER   TYPE     FINGERPRINT
gpu-host    user   jhahn   ed25519  SHA256:abc...
```

## See also

- [Fleet quick start](../agents/fleet-quickstart.md)
- [Agent deploy reference](../agents/deploy.md)
