# SSH — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to server-held SSH keys, known hosts and authorized keys.
> Feature folder: `docs/ssh/`.

The point of this feature: **the private key never leaves the server.** You ask
for a key, you take away its public half, and when something needs to
authenticate with it the server signs on your behalf.

## Prerequisites

- A running server, reachable over your chosen transport.
- A principal holding the SSH permissions. They are per-verb —
  `keys:create`, `keys:list`, `keys:get`, `keys:import` and their siblings —
  so read access and generation are separately grantable. `api.md` has the map.

## First run — a key you can deploy with

```bash
# 1. Generate. The private half is created and kept server-side.
quicbaas ssh keys generate --name fleet-deploy --type ed25519 --scope user

# 2. Take the public half.
quicbaas ssh keys export-public --name fleet-deploy --scope user --owner $USER

# 3. Install that public key in ~/.ssh/authorized_keys on the target host.

# 4. Use it, by name, without ever handling it.
quicbaas fleet deploy \
  --ssh user@target-host.example.com \
  --ssh-key-name fleet-deploy \
  --server https://quicbaas.example.com:18080 \
  --binary bin/quicbaas-static \
  --wait-online 2m
```

Step 3 is manual on purpose. `EmitAuthorizedKey` renders the `authorized_keys`
line for you; delivering it to the host is your business, because the server has
no privileged path onto a machine it has not yet been trusted by.

## Common tasks

### See what keys exist

```bash
quicbaas ssh keys list
quicbaas ssh keys get --name fleet-deploy
```

### Bring an existing key under management

```bash
quicbaas ssh keys import --name legacy-deploy
```

Import takes custody of a key that already exists. From that point the same rule
applies: there is no export of the private half. If you import a key you still
have a copy of elsewhere, the security benefit is only as good as your deletion
of that copy.

### Retire a key

```bash
quicbaas ssh keys delete --name fleet-deploy
```

Deleting removes the server's copy. It does **not** remove the public key from
any host's `authorized_keys` — those hosts will keep offering to accept it and
nothing will be able to answer. Remove it from the hosts too, or you have left
a dangling grant.

### Manage known hosts

```bash
quicbaas ssh known-hosts add
quicbaas ssh known-hosts list
quicbaas ssh known-hosts remove
```

Known hosts are how the server decides it is talking to the machine it meant to.
Treat write access to this store as security-relevant: whoever can add a record
can substitute a host.

### Manage authorized keys

```bash
quicbaas ssh authorized add [line]
quicbaas ssh authorized list
quicbaas ssh authorized remove
```

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| No verb exports the private key | Correct. There is none, by design. Use `export-public` and let the server sign. |
| Deploy fails with a handshake error after `export-public` | The public key is not in the target's `authorized_keys`, or it landed for the wrong user. Step 3 is manual. |
| `permission denied` on `generate` but `list` works | Permissions are per verb. `keys:create` is separate from `keys:list`. |
| A key still works after `keys delete` | It does not — but the host still advertises it. Remove the line from the target's `authorized_keys`. |
| Looking for `ssh session dial` or a console | Deliberately unimplemented. The key store is not an SSH jump host. See `architecture.md`. |

## What this feature does not do

- **No interactive sessions, no port forwarding, no agent forwarding.**
- **No automatic rotation.** Generate a new key, distribute it, delete the old
  one.
- **No pushing public keys to hosts.** Rendering the line is as far as it goes.
