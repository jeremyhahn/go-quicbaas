# Data → Objects

## Purpose

The Objects view manages blob storage: upload, download, delete, copy, and organize objects with tagging and lifecycle operations. Use this view to store files, documents, or binary data, then manage retention, replication, and archival policies.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "Browse", "Upload", "Upload Batch", "Settings"

**Tab: Browse**

```
Search / Filter:      [________]  (by key prefix)
Backend:              [Default ▼]  (storage backend)
Sort By:              [Name ▼]  (or Size, Modified)

Objects:

Key                    Size      Modified            Actions
─────────────────────────────────────────────────────────────
documents/report.pdf   2.5 MB    2026-04-25T14:30Z  [⋮]
documents/summary.txt  48 KB     2026-04-25T14:29Z  [⋮]
logs/app-2026-04.log   1.2 GB    2026-04-25T14:00Z  [⋮]

Total Size: 3.75 GB | Object Count: 3
```

- **Search** — client-side filter by key prefix
- **Backend** — select storage backend if multiple are available
- **Sort** — by name, size, or modification time
- **Objects** — table with three-dot menu per object

**Tab: Upload**

```
Select File:   [Choose File...]  or drag-drop
Object Key:    [documents/report.pdf______________]
                (defaults to filename; customize as needed)
Tags:          [env:prod, team:analytics________]
                (optional; key:value format)
Storage Class: [Standard ▼]  (or Infrequent, Archive)
Lifecycle:     Enable automatic [Days: 365]  archival
               after X days

               [Clear] [Upload]
```

- **Select File** — browse or drag-drop
- **Object Key** — full path; defaults to the filename
- **Tags** — optional metadata for filtering
- **Storage Class** — hot (Standard), warm (Infrequent), or cold (Archive)
- **Lifecycle** — optional auto-archival rule

**Tab: Upload Batch**

```
Select Files:  [Choose Files...] (multiple; shift+click or drag-drop)

Or paste JSON:
[
  {"key":"documents/report.pdf", "tags":{"env":"prod"}},
  {"key":"documents/summary.txt", "tags":{"env":"prod"}}
]

Storage Class: [Standard ▼]
               [Upload Batch]
```

- Bulk upload with optional JSON config

**Tab: Settings**

```
Object Subsystem Status: ✓ Enabled

Available Backends:
  Default (PebbleDB): 500 GB free
  S3 (aws): 1 TB free
  GCS (gcp): Unlimited (quota: 100 GB/month)

Replication:  [Enabled ▼]
  Target:     [S3 ▼]

Compression:  ✓ Enabled

[Save Settings]
```

- Shows available backends and replication settings

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Upload Object | Upload | Stores a file with optional tags and lifecycle policy | `PUT /api/v1/objects/{key}` |
| Download | Download | Downloads an object to the browser or local filesystem | `GET /api/v1/objects/{key}` |
| Delete | Trash | Removes an object | `DELETE /api/v1/objects/{key}` |
| Copy | Copy | Creates a copy of the object at a new key | `POST /api/v1/objects/{key}/copy` |
| Replicate | Sync | Copies the object to a target storage backend | `POST /api/v1/objects/{key}/replicate` |
| Archive | Archive | Moves the object to cold storage (if configured) | `POST /api/v1/objects/{key}/archive` |
| Restore | Restore | Retrieves an archived object back to hot storage | `POST /api/v1/objects/{key}/restore` |
| View Policy | Eye | Shows the object's retention, replication, and archival settings | `GET /api/v1/objects/{key}/policy` |

## Walkthrough

### Upload and download your first object

1. Open the Objects view (`#/data/objects`).
2. Click the **Upload** tab.
3. Click **Choose File...** and select a text file (e.g., `report.txt`) from your computer.
4. **Object Key** auto-fills with the filename. You can customize it, e.g., `documents/report.txt`.
5. **Tags** is optional. Leave empty for now.
6. **Storage Class** defaults to "Standard".
7. Leave **Lifecycle** unchecked for now.
8. Click **Upload**.

**Expected outcome:**

```
✓ Progress bar appears
✓ After upload completes, success toast: "Object uploaded: documents/report.txt"
✓ Tab resets
```

9. Click the **Browse** tab.
10. Your new object appears in the table:

```
Key                    Size      Modified
────────────────────────────────────────────
documents/report.txt   2.3 KB    2026-04-25T14:35Z  [⋮]
```

11. Click the **[⋮]** (three-dot menu) for your object.
12. Select **Download**.

**Expected outcome:**

```
✓ Browser downloads the file as "report.txt"
```

### Copy and replicate objects

13. From the three-dot menu, select **Copy**.
14. A dialog appears:

```
Copy Object

New Key:  [documents/report-backup.txt_____]

[Cancel] [Copy]
```

15. Change the key to `documents/report-backup.txt`.
16. Click **Copy**.

**Expected outcome:**

```
✓ Success toast: "Object copied: documents/report-backup.txt"
✓ Two objects now appear in Browse: report.txt and report-backup.txt
```

17. From the three-dot menu, select **Replicate** (if your setup has multiple backends).
18. A dialog appears:

```
Replicate Object

Target Backend:  [S3 ▼]

[Cancel] [Replicate]
```

19. Select a target backend (e.g., "S3") and click **Replicate**.

**Expected outcome:**

```
✓ Success toast: "Object replicated to S3"
✓ Object now exists on both backends
```

### Manage object policies

20. From the three-dot menu, select **View Policy**.
21. A modal shows the object's settings:

```
Object Policy: documents/report.txt

Retention:
  Retain Until: never (or a specific date)
  Locked: [No ▼]

Replication:
  Enabled: ✓
  Target: S3
  Sync Interval: hourly

Archival:
  Auto-archive: [Days: 365]

[Edit] [Delete]
```

22. Click **Edit** to modify the policy (e.g., set auto-archival to 90 days instead of 365).

### Delete an object

23. From the three-dot menu of the backup object, select **Delete**.
24. A confirm modal appears:

```
Delete Object?

documents/report-backup.txt will be permanently removed.

[Cancel] [Delete]
```

25. Click **Delete**.

**Expected outcome:**

```
✓ Success toast: "Object deleted: documents/report-backup.txt"
✓ Backup object disappears from Browse table
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Upload button is disabled | No file selected | Click "Choose File..." and select a file |
| Upload fails with "Key already exists" | An object with that key already exists | Choose a different key name; or check the "Overwrite" option if available |
| Download button appears disabled | Object is archived or being restored | Wait for restoration to complete; or use the Restore action |
| Copy or Replicate fails | Target key already exists or backend is unreachable | Choose a new key; check network and backend status |
| Archival does not trigger automatically | Lifecycle policy not set or time has not elapsed | Edit the object policy; set a smaller number of days for testing |
| Restore is slow | Cold storage provider is slow | This is normal for cold storage (can take hours); check provider status |
| Browse tab shows "No objects" | No objects uploaded yet | Click Upload and follow the walkthrough |

## See also

- [CLI: quicbaas objects](../cli/objects.md) — manage objects via CLI
- [API: database.md](../api/database.md) — objects API reference
- [handlers_objectstorage.go](../../pkg/api/rest/handlers_objectstorage.go) — object storage implementation
