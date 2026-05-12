# Security → Seal

## Purpose

The Seal view initializes the database seal, unseals the database, manages recovery keys, and rotates the root encryption key. Use this view during initial setup, after system recovery, and for key rotation ceremonies. The seal is the fundamental mechanism that protects the database's master encryption key.

## Layout

The page displays the current seal status and management controls:

**Status Section** (top)

```
Seal Status:           🔒 SEALED  (or 🔓 UNSEALED)
Initialized:           ✓ Yes (or ⚠ No — first-time setup required)
Master Key Status:     ✓ Protected (or ⚠ Compromised — rekey required)
Recovery Keys:         ✓ Stored securely
Last Rotation:         2026-01-15T08:00Z (or Never)

[Unseal] (if sealed) [Seal] (if unsealed) [Rekey] [Rotate Root Key]
```

- **Seal Status** — SEALED (red lock) or UNSEALED (green unlock)
- **Initialized** — whether seal has been initialized
- **Master Key Status** — Protected or Compromised (affects available actions)
- **Recovery Keys** — whether recovery keys exist
- **Last Rotation** — timestamp of last root key rotation

**If Status is "Not Initialized"** — shows:

```
Seal not initialized.

This is required for the first-time setup. Choose a seal backend:

Seal Backend:  [Filesystem ▼]  (or Vault, CloudKMS, YubiHSM, etc.)
               [Configure...]

[Initialize Seal]
```

**If Status is "Sealed"** — shows Unseal form:

```
Unseal Database

Seal Backend:    [Filesystem]  (read-only; shows configured backend)
Unseal Key:      [••••••••••________] or [Choose File...]
                 (depending on backend; may be a key share, password, or file)

Additional Shares: (if Shamir is configured)
  Share 1: [••••••••••________]
  Share 2: [••••••••••________]
  Share 3: [••••••••••________]
  (Need 3 of 5 shares entered)

                 [Unseal]
```

**Tab: Recovery Keys** (or expandable section)

```
Recovery Keys:

Stored Locally: ✓ Yes
Last Generated: 2026-01-15T08:00Z
Key Count:      1 (single recovery key)

Key ID               Generated             Status    Actions
──────────────────────────────────────────────────────────
rec_abc123def456     2026-01-15T08:00Z     active    [Download] [Rotate] [Delete]

[Generate New Recovery Key]
```

- Lists recovery keys (used for emergency recovery)
- Options to generate, rotate, or delete

**Tab: Root Key** (or expandable section)

```
Root Key Rotation:

Current Root Key:   rk_2026-01-15_v1
Created:            2026-01-15T08:00Z
Age:                3 months 10 days
Recommended Age:    1 year (rotate annually)

Status: ✓ Healthy (no rotation needed yet)

[Rotate Root Key]
```

- Shows current root key age and rotation status

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Initialize Seal | Lock | Initializes the seal with a backend (first-time only) | `POST /v1/seal/init` |
| Unseal Database | Unlock | Unseals with key shares or password (if sealed) | `POST /v1/seal/unseal` |
| Seal Database | Lock | Seals the database (only if unsealed) | `POST /v1/seal/seal` |
| Rekey | Refresh | Changes the master key by splitting it into new shares | `POST /v1/seal/rekey` |
| Rotate Root Key | Sync | Rotates the root encryption key (long-term protection) | `POST /v1/seal/rotate-root` |
| Generate Recovery Key | Plus | Creates a new emergency recovery key | `POST /v1/seal/recovery/generate` |
| Delete Recovery Key | Trash | Removes a recovery key (shows warning) | `DELETE /v1/seal/recovery` |

## Walkthrough

### First-time initialization

1. Open the Seal view (`#/security/seal`).
2. If the view shows "Seal not initialized", you're in first-time setup mode.
3. **Seal Backend** dropdown shows options:

```
[Filesystem ▼]
  Filesystem (local file in data-dir/seal/)
[Vault ▼]
  HashiCorp Vault (external KMS)
[CloudKMS ▼]
  Google Cloud KMS
[YubiHSM ▼]
  Yubico YubiHSM 2
[AzureKeyVault ▼]
  Azure Key Vault
```

4. For testing, select **Filesystem**.
5. Click **[Configure...]** to set options (e.g., number of key shares, threshold).
6. A modal appears:

```
Configure Seal Backend: Filesystem

Key Shares:      [5]  (total number of shares)
Threshold:       [3]  (shares needed to unseal)
Location:        [/data/seal/keys]  (auto-filled)

[Confirm]
```

7. Leave defaults (5 shares, 3 threshold — need 3 of 5 to unseal).
8. Click **[Confirm]**.

**Expected outcome:**

```
✓ Modal closes
✓ "Initialize Seal" button becomes available
```

9. Click **[Initialize Seal]**.
10. A multi-step modal appears:

```
Step 1: Initialize

Initializing seal with Filesystem backend...
5 key shares will be generated.
You need 3 shares to unseal.

[Next] [Cancel]
```

11. Click **[Next]**.
12. Step 2 shows the generated key shares (write them down or save them securely):

```
Step 2: Save Key Shares

IMPORTANT: Save these key shares in a secure location.
Each share should be stored separately.

Share 1: U8d7f9K2j4H6g5L9N3Q8w2E5R6T7Y8U9I...
Share 2: K4m1O7p9L2j3H5g6F8d2E4R9T7Y6U5I3Q...
Share 3: ...
Share 4: ...
Share 5: ...

[✓] I have saved all 5 shares
[Next] [Cancel]
```

13. Copy and save each share to a secure location (password manager, hardware storage, etc.).
14. Check the "I have saved..." checkbox.
15. Click **[Next]**.
16. Step 3 generates and shows the recovery key:

```
Step 3: Recovery Key

Save this recovery key for emergency recovery.

Recovery Key: rec_abc123def456_XXXXXX...

[Download] [Copy]

[✓] I have saved the recovery key
[Next] [Cancel]
```

17. Save the recovery key securely.
18. Check the checkbox and click **[Next]**.
19. Step 4 confirms:

```
Step 4: Complete

Seal has been initialized and the database is now UNSEALED.

Status: 🔓 UNSEALED

Important next steps:
1. Rotate or delete the recovery key monthly for operational security
2. Back up your key shares in separate, secure locations
3. Test the unseal process (seal, then unseal)

[Done]
```

20. Click **[Done]**.

**Expected outcome:**

```
✓ Seal Status shows: 🔓 UNSEALED
✓ "Initialized" shows: ✓ Yes
```

### Seal and unseal the database

21. After initialization, click **[Seal]** to seal the database (optional; for testing).
22. A confirm modal appears:

```
Seal the Database?

The database will be locked. You will need key shares to unseal it.

[Cancel] [Seal]
```

23. Click **[Seal]**.

**Expected outcome:**

```
✓ Status immediately shows: 🔒 SEALED
✓ All Unseal form fields appear
```

24. Click **[Unseal]** to unseal it again (demonstrate the process).
25. An Unseal form appears:

```
Unseal Database

Share 1: [••••••••••________]  (paste first key share)
Share 2: [••••••••••________]  (paste second key share)
Share 3: [••••••••••________]  (paste third key share)

[Clear] [Unseal]
```

26. Paste 3 of your saved key shares.
27. Click **[Unseal]**.

**Expected outcome:**

```
✓ Status updates: 🔓 UNSEALED
✓ Success toast: "Database unsealed"
```

### Rotate the root key

28. Click the **Root Key** tab (or scroll down).
29. You see:

```
Current Root Key:   rk_2026-01-15_v1
Created:            2026-01-15T08:00Z
Age:                3 months 10 days
Status: ✓ Healthy
```

30. Once per year (or after a security incident), click **[Rotate Root Key]**.
31. A confirm modal appears:

```
Rotate Root Encryption Key?

A new root key will be generated and the old key will be retired.
This operation takes 1-5 minutes depending on data size.

[Cancel] [Rotate]
```

32. Click **[Rotate]**.

**Expected outcome:**

```
✓ Progress bar shows rotation in progress
✓ After completion, success toast: "Root key rotated"
✓ Current Root Key updates to new version and timestamp
✓ Age resets to 0 days
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| "Seal is already initialized" | Seal was already set up previously | Skip initialization; go directly to Unseal if sealed |
| Unseal fails with "Invalid key share" | Key share is incorrect or corrupted | Verify the share is correct; re-copy from secure storage |
| Unseal requires more shares than expected | Configured threshold is higher than shares entered | Enter the correct number of shares; check configuration |
| "Master Key Status: Compromised" appears | Indicates a security issue | Immediately rekey or rotate root key; review security logs |
| Rekey fails | Master key is compromised; rekey is locked | Restore from backup or contact support |
| Root key rotation is slow | Large database or slow disk I/O | This is normal; wait for completion; check disk health |
| Recovery key cannot be generated | Backend does not support recovery keys | Some backends (YubiHSM) have limited recovery support; check documentation |
| Delete recovery key fails | Recovery key is in use or being validated | Wait and try again; or regenerate a new recovery key first |

## See also

- [CLI: quicbaas seal](../cli/seal.md) — seal operations via CLI
- [handlers_seal.go](../../pkg/api/rest/handlers_seal.go) — seal implementation
- [security/seal.md](../security/seal.md) — seal architecture and best practices
- [Security → KMS](security-kms.md) — key management service
- [security/kms.md](../security/kms.md) — encryption key management
