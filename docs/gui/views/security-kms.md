# Security → KMS

## Purpose

The KMS (Key Management Service) view manages encryption keys: create keys, rotate them, view versions, set key policies, mark defaults, and manage key safety limits. Use this view to govern the cryptographic keys used throughout QuicBaaS for at-rest encryption, AEAD operations, and cryptographic signing.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "Keys", "Key Versions", "Safety Limits", "Policies"

**Tab: Keys**

```
Search: [________]  (by key name)

Create Key: [+ Generate Key]

Keys:

Key Name              Type       Status    Default  Created            Actions
──────────────────────────────────────────────────────────────────────────────
master-key-2026      RSA-4096   enabled   ★        2026-01-01T00:00Z  [⋮]
data-encryption      AES-256    enabled   —        2026-02-15T10:00Z  [⋮]
old-key-2025         RSA-2048   disabled  —        2025-12-01T08:00Z  [⋮]

[Generate] [Rotate] [Set Default] [Disable] [Delete]  (per key)
```

- **Key Name** — unique identifier
- **Type** — RSA-2048, RSA-4096, AES-256, etc.
- **Status** — enabled (green), disabled (gray), compromised (red)
- **Default** — star icon if this is the system default key
- **Created** — ISO 8601 timestamp
- Three-dot menu for actions

**Tab: Key Versions**

```
Key Name: [master-key-2026 ▼]  (dropdown to select key)

Versions:

Version   Status     Created            Rotated By   Actions
──────────────────────────────────────────────────────────
v5        active     2026-04-20T12:00Z  system       [Export] [Use] [Delete]
v4        retired    2026-04-10T12:00Z  admin        [Export] [Delete]
v3        retired    2026-03-31T12:00Z  admin        [Export] [Delete]
v2        archived   2026-03-01T12:00Z  system       [Export]
v1        archived   2026-01-01T00:00Z  system       [Export]

[Rotate Key]  (generates v6)
```

- **Version** — sequential version number
- **Status** — active (current), retired (old), archived (very old)
- **Created** — when version was created
- **Rotated By** — system or admin name who initiated rotation
- **Actions** — Export, Use (set active), Delete

**Tab: Safety Limits**

```
Key Name: [master-key-2026 ▼]

AEAD Safety Limits:

Encryptions Performed:  450,234 / 2^32 (limit)  [🟢 Safe]
Data Encrypted:         12.3 GB / unlimited

Danger Zone:            < 2^30 encryptions remaining

Current Status: ✓ Safe (87% under safety limit)
Last Check:     2026-04-25T14:30:00Z

[Check Now] [Update Limits] [Clear Limits]
```

- Shows current key usage against safety limits
- Status indicator: Safe (green), Warning (yellow), Exceeded (red)
- Buttons to update or clear limits

**Tab: Policies**

```
Key Name: [master-key-2026 ▼]

Key Policy (JSON):

{
  "version": "2012-10-17",
  "statement": [
    {
      "effect": "allow",
      "principal": "role:admin",
      "action": ["kms:decrypt", "kms:encrypt"],
      "resource": "*"
    }
  ]
}

[Edit] [Save] [Reset to Default]
```

- Raw JSON policy editor for fine-grained access control

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Generate Key | Plus | Opens a form to create a new encryption key | `POST /v1/kms/keys` |
| Rotate Key | Sync | Creates a new version of the key | `POST /v1/kms/keys/{name}/rotate` |
| Set Default | Star | Marks a key as the system default for new encryptions | `PUT /v1/kms/keys/{name}/default` |
| Enable/Disable | Toggle | Toggles key status (enabled = usable, disabled = blocked) | `PUT /v1/kms/keys/{name}/status` |
| Delete Key | Trash | Removes a key (show warning if default or in use) | `DELETE /v1/kms/keys/{name}` |
| Export Key | Download | Exports a key wrapped with a wrapping key (for backup) | `POST /v1/kms/keys/{name}/export` |
| Import/Unwrap | Upload | Imports a previously exported key | `POST /v1/kms/keys/unwrap` |
| Set Safety Limits | Edit | Configures AEAD safety thresholds | `PUT /v1/kms/keys/{name}/safety` |
| Update Policy | Save | Saves key access control policy | `PUT /v1/kms/keys/{name}/policy` |

## Walkthrough

### Generate your first encryption key

1. Open the KMS view (`#/security/kms`).
2. Click the **Keys** tab.
3. Click **+ Generate Key** button.
4. A form modal appears:

```
Generate Encryption Key

Key Name:       [production-dek______________]  (DEK = Data Encryption Key)
Key Type:       [AES-256 ▼]  (or RSA-2048, RSA-4096, ED25519, etc.)
Rotation Policy: [Annual ▼]  (or Never, Quarterly, Monthly, On-Demand)

Description:    [Production data encryption key_____________]
                (optional)

Set as Default: [✓] (checkbox; makes this the system default)

[Cancel] [Generate]
```

5. Enter **Key Name**: `production-dek`.
6. **Key Type** defaults to "AES-256"; leave it (good for data encryption).
7. **Rotation Policy**: Set to "Annual" (rotate once per year).
8. **Description**: `Production data encryption key`.
9. Check **Set as Default** to use this key for new encryptions.
10. Click **Generate**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Key generated: production-dek"
✓ Key appears in the Keys table with status "enabled" (green)
✓ Star icon (★) shows this is the default key
```

### Rotate a key

11. Find your key in the Keys table.
12. Click the three-dot menu and select **Rotate Key**.
13. A confirm modal appears:

```
Rotate Key: production-dek?

A new version will be created and marked active.
Old versions will be archived after 90 days.

[Cancel] [Rotate]
```

14. Click **Rotate**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Key rotated: production-dek"
✓ Key status remains "enabled" (green)
```

15. Click the **Key Versions** tab and select "production-dek" from the dropdown.
16. You now see two versions:

```
Version   Status     Created
──────────────────────────────
v2        active     2026-04-25T14:35Z   (newly created)
v1        retired    2026-04-20T12:00Z   (old version)
```

### Set a default key

17. Click the **Keys** tab.
18. Create a second key (e.g., `backup-dek`) by clicking **+ Generate Key** and repeating the steps but **unchecking** "Set as Default".
19. The new key appears with no star icon.
20. Click the three-dot menu and select **Set Default**.
21. A confirm modal appears:

```
Set Default Key?

backup-dek will become the system default.
All new encryptions will use this key.

[Cancel] [Set Default]
```

22. Click **Set Default**.

**Expected outcome:**

```
✓ Success toast: "Default key changed to backup-dek"
✓ Star icon moves from production-dek to backup-dek
✓ production-dek status remains "enabled" (can still decrypt old data)
```

### Check and update safety limits

23. Click the **Safety Limits** tab.
24. **Key Name** should show "production-dek" (or select from dropdown).
25. You see current usage:

```
Encryptions Performed:  1,234 / 2^32  (very small percentage)
Data Encrypted:         2.3 GB / unlimited
Current Status: ✓ Safe (99.99% under limit)
```

26. Click **Update Limits** to adjust thresholds if needed (e.g., lower the encryption count limit for more frequent rotation).
27. A form modal appears:

```
Update Safety Limits: production-dek

Max Encryptions:  [2^32 ▼]  (or custom limit)
Max Data Volume:  [Unlimited ▼]  (or custom limit in GB)
Alert Threshold:  [90% ▼]  (alert when usage exceeds this)

[Cancel] [Save]
```

28. Leave defaults for now. Click **Save**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Safety limits updated"
```

### Set a key policy

29. Click the **Policies** tab.
30. **Key Name** shows "production-dek" (or select from dropdown).
31. You see a JSON policy editor:

```json
{
  "version": "2012-10-17",
  "statement": [
    {
      "effect": "allow",
      "principal": "role:admin",
      "action": ["kms:decrypt", "kms:encrypt"],
      "resource": "*"
    }
  ]
}
```

32. Modify the policy to restrict decryption to admins only (keep encryption open):

```json
{
  "version": "2012-10-17",
  "statement": [
    {
      "effect": "allow",
      "principal": "role:admin",
      "action": ["kms:decrypt", "kms:encrypt"],
      "resource": "*"
    },
    {
      "effect": "allow",
      "principal": "role:user",
      "action": ["kms:encrypt"],
      "resource": "*"
    }
  ]
}
```

33. Click **Save**.

**Expected outcome:**

```
✓ Success toast: "Policy updated"
✓ Users with role:user can now encrypt but not decrypt
✓ Only role:admin can do both
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Generate Key form shows validation error | Key name is empty or already exists | Choose a unique key name |
| Rotate fails with "Key cannot be rotated" | Rotation policy is set to "Never" | Update the rotation policy or rotate manually |
| Set Default fails | Key is disabled | Enable the key first; then set as default |
| Delete fails with "Key is in use" | Key is the default or currently encrypting data | Set a different key as default; wait for pending operations to complete |
| Safety limits show "Exceeded" (red) | AEAD operation count has reached the limit | Rotate the key immediately; update limits if needed |
| Policy JSON shows parse error | JSON syntax is invalid | Fix curly braces, quotes, commas; use a JSON validator |
| Export fails | Key export is not allowed by policy | Check key policy; ensure your role has kms:export permission |

## See also

- [CLI: quicbaas kms](../cli/kms.md) — manage KMS via CLI
- [handlers_kms.go](../../pkg/api/rest/handlers_kms.go) — KMS implementation
- [security/kms.md](../security/kms.md) — KMS architecture and design
- [security/kms-multi-tenancy.md](../security/kms-multi-tenancy.md) — multi-tenant KMS
- [Security → Secrets](security-secrets.md) — secret management
- [Security → Seal](security-seal.md) — database seal and recovery keys
