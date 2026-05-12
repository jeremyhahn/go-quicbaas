# Security → SSH

## Purpose

The SSH view manages SSH keypairs and known hosts for secure shell access. Use this view to generate new SSH keys, import existing keys, view public keys for authorized_keys files, and maintain a registry of trusted host fingerprints. SSH integration supports agent-based operations and remote deployments.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "SSH Keys", "Known Hosts"

**Tab: SSH Keys**

```
Scope:   [Global ▼]  (or User, Organization, etc.)
Owner:   [________] (filter by owner; optional)

Create Key: [+ Generate Key] [+ Import Key]

SSH Keys:

Name                 Type        Key Size  Owner       Created            Actions
────────────────────────────────────────────────────────────────────────────────
deploy-key-prod      RSA         4096      admin       2026-04-20T08:00Z  [⋮]
github-deploy        ED25519     256       admin       2026-04-15T14:00Z  [⋮]
user-laptop          RSA         2048      alice       2026-03-01T10:00Z  [⋮]

[Export Public Key] [Export Private Key] [Delete]  (per key)
```

- **Name** — unique key identifier
- **Type** — RSA, ED25519, ECDSA, etc.
- **Key Size** — bit length (2048, 4096, 256, etc.)
- **Owner** — user or role that owns the key
- **Created** — ISO 8601 timestamp
- Three-dot menu for actions

**Tab: Known Hosts**

```
Add Host:  [+ Add Known Host]

Known Hosts:

Host             Port  Key Type  Fingerprint (SHA256)                     Added      Actions
─────────────────────────────────────────────────────────────────────────────────────────────
github.com       22    RSA       SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw...  2026-01-15 [⋮]
gitlab.com       22    ECDSA     SHA256:HbW3g6CQhzrPCfx9L8mLa7TqY3E5vT9jN...       2026-02-20 [⋮]
internal-git     22    ED25519   SHA256:aBcD1EfG2hIj3KlM4nOpQrStUvWxYzA5...       2026-04-10 [⋮]

[Verify] [Delete]  (per host)
```

- **Host** — hostname or IP address
- **Port** — SSH port (usually 22)
- **Key Type** — host key algorithm
- **Fingerprint** — SHA256 hash of the public key for verification
- **Added** — when this host was pinned
- Three-dot menu for actions

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Generate Key | Plus | Creates a new SSH keypair | `POST /ssh/keys/{scope}/{owner}/generate` |
| Import Key | Upload | Imports a PEM-encoded private key | `POST /ssh/keys/{scope}/{owner}/import` |
| Export Public Key | Download | Shows the public key in authorized_keys format | `GET /ssh/keys/{scope}/{owner}/{name}/public` |
| Export Private Key | Download | Downloads the private key (PEM-encoded, encrypted) | `GET /ssh/keys/{scope}/{owner}/{name}/private` |
| Delete Key | Trash | Removes a keypair; show warning if in use | `DELETE /ssh/keys/{scope}/{owner}/{name}` |
| Add Known Host | Plus | Pins a host's public key fingerprint | `POST /known-hosts` |
| Verify Host | Check | Verifies the stored fingerprint against the current host key | `POST /known-hosts/{host}/verify` |
| Delete Known Host | Trash | Removes a pinned host from the known hosts registry | `DELETE /known-hosts/{host}` |

## Walkthrough

### Generate your first SSH keypair

1. Open the SSH view (`#/security/ssh`).
2. Click the **SSH Keys** tab.
3. Click **+ Generate Key** button.
4. A form modal appears:

```
Generate SSH Keypair

Name:           [deploy-prod______________]
Type:           [RSA ▼]  (or ED25519, ECDSA)
Key Size:       [4096 ▼]  (2048, 3072, 4096 for RSA; 256 for ED25519)
Owner:          [admin__________]  (current user or role)
Passphrase:     [••••••••••________]  (optional; protects private key)
                [Generate passphrase] or [Leave empty]

Description:    [Production deployment key__________]  (optional)

[Cancel] [Generate]
```

5. Enter **Name**: `deploy-prod`.
6. **Type**: Select "RSA" (most compatible).
7. **Key Size**: Select "4096" (strong encryption).
8. **Owner**: Defaults to current user; leave as-is.
9. **Passphrase**: Optional. For testing, leave empty. In production, set a strong passphrase.
10. **Description**: `Production deployment key`.
11. Click **[Generate]**.

**Expected outcome:**

```
✓ Modal shows generation progress
✓ After completion, displays the new keypair summary:

  Key: deploy-prod (RSA-4096)
  Owner: admin
  Fingerprint (SHA256): aBcD1EfG2hIj3KlM4nOpQrStUvWxYzA5bCdEfGhIjK...
  Created: 2026-04-25T14:35:00Z

  ✓ I have securely stored the private key
  [Download Private Key] [Copy Public Key] [Done]
```

12. Click **[Download Private Key]** to save it securely (e.g., `~/.ssh/deploy-prod`).

**Expected outcome:**

```
✓ Browser downloads the PEM-encoded private key
✓ File: deploy-prod (or deploy-prod.pem)
```

13. Click **[Copy Public Key]** to copy the authorized_keys-format public key to clipboard.

**Expected outcome:**

```
✓ Public key copied: ssh-rsa AAAAB3NzaC1yc2EAAA...
✓ Can paste into ~/.ssh/authorized_keys on remote hosts
```

14. Click **[Done]**.

**Expected outcome:**

```
✓ Modal closes
✓ SSH Keys table now shows:
  deploy-prod   RSA      4096     admin      2026-04-25T14:35Z  [⋮]
```

### Import an existing SSH key

15. Click **+ Import Key**.
16. A form modal appears:

```
Import SSH Key

Name:         [github-deploy______________]
Scope:        [Global ▼]
Owner:        [admin__________]

Private Key:  [Paste PEM-encoded key here...
               -----BEGIN OPENSSH PRIVATE KEY-----
               ...
               -----END OPENSSH PRIVATE KEY-----]
               or [Choose File...]

Passphrase:   [••••••••••________]  (if key is passphrase-protected)

[Cancel] [Import]
```

17. Enter **Name**: `github-deploy`.
18. Paste or upload an existing private key (PEM or OpenSSH format).
19. If the key has a passphrase, enter it in **Passphrase**.
20. Click **[Import]**.

**Expected outcome:**

```
✓ Modal closes
✓ Key appears in the SSH Keys table
✓ Success toast: "Key imported: github-deploy"
```

### Export a public key

21. Find a key in the SSH Keys table (e.g., `deploy-prod`).
22. Click the three-dot menu and select **Export Public Key**.
23. A modal shows the public key in authorized_keys format:

```
Public Key: deploy-prod (RSA-4096)

ssh-rsa AAAAB3NzaC1yc2EAAA...

[Copy] [Download]
```

24. Click **[Copy]** to copy to clipboard for pasting into remote `~/.ssh/authorized_keys`.

### Pin a known host

25. Click the **Known Hosts** tab.
26. Click **+ Add Known Host**.
27. A form modal appears:

```
Add Known Host

Host:       [github.com______________]
Port:       [22]  (SSH port, usually 22)

Fetch Fingerprint:  [Fetch from Host]  (auto-detect)
  or
Manual Fingerprint: [SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLvi...]

[Cancel] [Add]
```

28. Enter **Host**: `github.com`.
29. **Port**: Defaults to 22; leave it.
30. Click **[Fetch from Host]** to auto-detect the host's SSH key fingerprint.

**Expected outcome:**

```
✓ Progress indicator shows "Fetching..."
✓ After 1-2 seconds, auto-populated Fingerprint:
  Key Type: RSA
  SHA256: nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw...
```

31. Click **[Add]**.

**Expected outcome:**

```
✓ Modal closes
✓ Host appears in Known Hosts table:
  github.com   22    RSA       SHA256:nThbg6kXUpJWGl7E...   2026-04-25 [⋮]
```

### Verify a known host

32. Find the known host in the table (e.g., `github.com`).
33. Click the three-dot menu and select **Verify**.
34. A modal appears:

```
Verify Host: github.com

Stored Fingerprint (SHA256):
  nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw...

Current Fingerprint (from Host):
  nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw...

Status: ✓ MATCH

[Close]
```

**Expected outcome:**

```
✓ Status shows "MATCH" (green checkmark)
✓ The host's current key matches the stored fingerprint
```

35. If they don't match, the status shows "MISMATCH" (red X) — indicates a potential MITM attack; do not proceed.

### Delete a key or host

36. To delete an SSH key, click the three-dot menu and select **Delete**.
37. A confirm modal appears:

```
Delete SSH Key: deploy-prod?

This cannot be undone. Make sure you have a backup of the private key.

[Cancel] [Delete]
```

38. Click **[Delete]**.

**Expected outcome:**

```
✓ Success toast: "Key deleted: deploy-prod"
✓ Key disappears from the table
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Generate Key button is disabled | Required fields are empty | Fill in Name and Type |
| Import Key fails with "Invalid PEM format" | Key file is not a valid PEM-encoded SSH key | Verify file format; use `ssh-keygen -p -N "" -m pem -f keyfile` to convert |
| Export Private Key fails with "Access denied" | User does not have permission to export | Check IAM role; ensure you have `ssh:export` permission |
| Fingerprint Fetch fails with "Cannot connect to host" | Host is unreachable or SSH port is not 22 | Verify hostname/IP and port; check network connectivity |
| Verify Host shows "MISMATCH" | Host key has changed (normal after server updates) | Update the stored fingerprint by deleting and re-adding the host |
| Cannot delete a key | Key is in use by an active session or agent | Revoke the key's use before deleting; or wait for sessions to close |

## Keyboard Shortcuts

- None specific to SSH view (uses standard copy, paste, download)

## Accessibility Notes

- SSH key values are masked by default for security; use [Show] toggle to reveal
- Copy buttons have aria-labels for screen readers
- Fingerprint comparison view includes color-coded status (green/red) with text labels for colorblind users

## See also

- [CLI: quicbaas ssh](../cli/ssh.md) — manage SSH keys via CLI
- [handlers_ssh.go](../../pkg/api/rest/handlers_ssh.go) — SSH key implementation
- [agents/deploy.md](../agents/deploy.md) — SSH integration for agent deployment
- [security/authentication.md](../security/authentication.md) — authentication mechanisms
