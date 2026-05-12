# Security → Secrets

## Purpose

The Secrets view stores, rotates, and versions sensitive data (API keys, passwords, credentials) with automatic encryption via the KMS. Use this view to create secrets, retrieve them, rotate sensitive values, and manage secret versions and metadata.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "Secrets", "Versions", "Audit", "Settings"

**Tab: Secrets**

```
Search: [________]  (by secret path or name)

Create Secret: [+ New Secret]

Secrets:

Path                         Type       Status    Rotated    Last Access      Actions
────────────────────────────────────────────────────────────────────────────────────
db/prod/password             string     enabled   2026-04-20 2026-04-25T14:00Z [⋮]
api-keys/github              string     enabled   Never      2026-04-25T13:00Z [⋮]
tls/ca-cert                  certificate enabled  Never     2026-01-15T10:00Z [⋮]
oauth/client-secret          string     disabled  2026-02-28 —                 [⋮]

[View] [Rotate] [Enable/Disable] [Delete]  (per secret)
```

- **Path** — hierarchical identifier (e.g., `db/prod/password`)
- **Type** — string, certificate, key-pair, json, etc.
- **Status** — enabled (green), disabled (gray)
- **Rotated** — last rotation date or "Never"
- **Last Access** — ISO 8601 timestamp or "—"
- Three-dot menu for actions

**Tab: Versions**

```
Secret Path: [db/prod/password ▼]  (dropdown to select secret)

Versions:

Version   Created            Rotated By   Status     Actions
───────────────────────────────────────────────────────────
v3        2026-04-20T08:00Z  admin        current    [Use as Current] [Delete]
v2        2026-03-20T08:00Z  system       retired    [Use as Current] [Delete]
v1        2026-02-20T08:00Z  admin        archived   [Delete]

[Rollback] (to a previous version)
```

- **Version** — sequential version number
- **Created** — when version was created
- **Rotated By** — who initiated rotation (system or admin name)
- **Status** — current (in use), retired (old), archived (very old)
- **Actions** — Use as Current (rollback), Delete

**Tab: Audit**

```
Secret Path: [db/prod/password ▼]

Audit Log (read-only):

Timestamp              Action          User           Result
──────────────────────────────────────────────────────────────
2026-04-25T14:00:00Z   read           admin          ✓ Success
2026-04-25T13:30:00Z   rotate         system         ✓ Success
2026-04-25T12:00:00Z   read           client-app     ✓ Success
2026-04-20T08:00:00Z   create         admin          ✓ Success

[Export Audit Log] [Filter by Action]
```

- **Timestamp** — ISO 8601 timestamp
- **Action** — create, read, rotate, delete, enable, disable
- **User** — who performed the action (admin, system, client app name)
- **Result** — Success (green), Failed (red)

**Tab: Settings**

```
Secrets Configuration:

Encryption Key:       [production-dek ▼]  (KMS key used for encryption)
Rotation Policy:      [90 days ▼]  (recommend or enforce rotation interval)
Audit Logging:        [✓] Enabled
Max Secret Size:      [1 MB ▼]
Automatic Rotation:   [Disabled ▼]  (or Enabled with interval)

Access Control:
  Default TTL:        [1 hour ▼]  (how long secret is cached in memory)
  Require MFA:        [Disabled ▼]  (for sensitive secrets)

                      [Save Settings]
```

- Global configuration for secret management

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create Secret | Plus | Opens a form to store a new secret | `POST /v1/secrets/{path}` |
| Retrieve Secret | Eye | Displays the secret value (masked by default) | `GET /v1/secrets/{path}` |
| Rotate Secret | Sync | Creates a new version; optionally invalidates old ones | `POST /v1/secrets/{path}/rotate` |
| List Secrets | List | Lists all secrets (path only; values hidden) | `GET /v1/secrets` |
| List Versions | History | Shows all versions of a secret | `GET /v1/secrets/{path}/versions` |
| Rollback | Undo | Activates a previous version as current | `POST /v1/secrets/{path}/rollback` |
| Enable/Disable | Toggle | Toggles secret access (disabled = blocked) | `POST /v1/secrets/{path}/enable` or `/disable` |
| Delete Secret | Trash | Removes all versions of a secret; show confirm modal | `DELETE /v1/secrets/{path}` |
| Export Audit Log | Download | Downloads audit entries as CSV | — (client-side export) |

## Walkthrough

### Create your first secret

1. Open the Secrets view (`#/security/secrets`).
2. Click the **Secrets** tab.
3. Click **+ New Secret** button.
4. A form modal appears:

```
Create Secret

Path:         [db/prod/password________________]  (hierarchical; use / separator)
Type:         [String ▼]  (or Certificate, Key-Pair, JSON, etc.)

Secret Value: [••••••••••] (password input; masked)
              [Show] (toggle to reveal)

Metadata:
  Description: [Production database password_____]  (optional)
  Tags:        [env:prod, app:api__________]  (optional)
  Auto-Rotate: [Enabled ▼] Every [90 days]

Encryption:   [production-dek ▼]  (KMS key to use)

              [Cancel] [Create]
```

5. Enter **Path**: `db/prod/password`.
6. **Type** defaults to "String"; leave it.
7. In **Secret Value**, enter a password (e.g., a database password or use a generated one).
8. Click **[Show]** to verify it's correct.
9. **Description**: `Production database password`.
10. **Tags**: `env:prod,app:api`.
11. **Auto-Rotate**: Leave "Enabled" with "90 days" (automatic rotation every 90 days).
12. **Encryption**: Defaults to "production-dek" (your KMS key); leave it.
13. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Secret created: db/prod/password"
✓ Secret appears in the Secrets table with status "enabled" (green)
✓ "Rotated" shows "Never" (new secret, not yet rotated)
```

### Retrieve a secret

14. Find your secret in the table and click its row.
15. A detail panel opens showing:

```
Secret: db/prod/password

Path: db/prod/password
Type: string
Status: enabled
Created: 2026-04-25T14:35:00Z
Description: Production database password
Tags: env:prod, app:api

Secret Value: [••••••••••] (masked)
              [Show] [Copy] [Use in Prompt]

Versions: 1

[Rotate] [Disable] [Delete]
```

16. Click **[Show]** to reveal the secret value (may require MFA if configured).

**Expected outcome:**

```
✓ Secret value is displayed (unmasked)
✓ [Show] toggles back to [Hide]
```

17. Click **[Copy]** to copy to clipboard (useful for pasting into configs).

### Rotate a secret

18. From the secret detail panel, click **[Rotate]**.
19. A form modal appears:

```
Rotate Secret: db/prod/password

New Value: [••••••••••]  (or auto-generate)
           [Generate] [Show]

Invalidate Old Versions: [✓] (checkbox; retire versions after rotation)
  Retire After: [7 days ▼]  (grace period before old version is archived)

[Cancel] [Rotate]
```

20. Enter a new password or click **[Generate]** to create a random one.
21. Check **Invalidate Old Versions** to retire the old password (good practice).
22. Set "Retire After" to "7 days" (grace period for clients to update config).
23. Click **[Rotate]**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Secret rotated: db/prod/password"
✓ Secrets table updates: "Rotated" shows today's date
```

24. Click the **Versions** tab and select `db/prod/password`.
25. You now see two versions:

```
Version   Created            Rotated By   Status
───────────────────────────────────────────────
v2        2026-04-25T14:40Z  admin        current
v1        2026-04-25T14:35Z  admin        retired (will auto-archive in 7d)
```

### Rollback to a previous version

26. If you made a mistake and need to revert, click on version v1 in the Versions list.
27. Click **[Use as Current]**.
28. A confirm modal appears:

```
Rollback to version v1?

The old password will be activated. Systems using the new version may break.

[Cancel] [Rollback]
```

29. Click **[Rollback]**.

**Expected outcome:**

```
✓ Success toast: "Rolled back to v1"
✓ Status badge changes: v1 → current, v2 → retired
```

### Check audit logs

30. Click the **Audit** tab.
31. **Secret Path** shows `db/prod/password`.
32. The audit log displays all actions on this secret:

```
Timestamp              Action        User     Result
─────────────────────────────────────────────────────
2026-04-25T14:42:00Z   rollback      admin    ✓ Success
2026-04-25T14:40:00Z   rotate        admin    ✓ Success
2026-04-25T14:35:00Z   create        admin    ✓ Success
```

33. Click **[Export Audit Log]** to download audit entries as CSV.

**Expected outcome:**

```
✓ Browser downloads: secrets-audit-db-prod-password-2026-04-25.csv
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create Secret form shows validation error | Path is empty or invalid | Use a valid path like `db/prod/password`; avoid spaces |
| Cannot reveal secret | MFA is required but not set up | Skip [Show] or enable MFA and log in |
| Rotate fails with "Cannot auto-generate password" | Type is not a string (e.g., certificate) | Generate the new value manually; or use `[Generate]` for compatible types |
| Rollback fails with "Only one version" | Secret has only one version; cannot rollback | You cannot rollback a secret with no history |
| Delete confirms but secret still appears | Audit cache is stale | Refresh the page |
| Auto-rotation does not trigger | Automatic rotation is disabled in settings | Enable auto-rotation in the Settings tab |
| Secret size exceeds 1 MB | Value is too large for the configured limit | Compress the value; or increase max size in Settings (may require restart) |

## See also

- [CLI: quicbaas secrets](../cli/secrets.md) — manage secrets via CLI
- [handlers_secrets.go](../../pkg/api/rest/handlers_secrets.go) — secrets implementation
- [security/secrets.md](../security/secrets.md) — secrets architecture and design
- [security/audit.md](../security/audit.md) — audit logging
- [Security → KMS](security-kms.md) — key management (encrypt secrets)
