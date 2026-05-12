# Data → KV Store

## Purpose

The KV Store view provides a graphical interface for entity-typed key-value operations: put (store), get (retrieve), delete, and list operations. Use this view to manage application data, configurations, or test the KV layer without using the CLI or API directly.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "Put", "Get", "List", "Delete"

**Tab: Put**

```
Entity Type:  [String ▼]        (or Binary, JSON, etc.)
Key:          [my-config-key________]
Value:        [{"color":"blue"}________]
              (or text area for large values)

              [Clear] [Store]
```

- **Entity Type** — dropdown selecting the value type (String, Binary, JSON, Protobuf, etc.)
- **Key** — string key name
- **Value** — text area or structured editor depending on type
- **Store** button — saves the key-value pair

**Tab: Get**

```
Entity Type:  [String ▼]
Key:          [my-config-key________]

              [Search]

Result:
{
  "key": "my-config-key",
  "value": {"color":"blue"},
  "type": "json",
  "created": "2026-04-25T14:30:00Z",
  "version": 1
}

[Copy] [Delete]
```

- **Search** button — retrieves the key
- **Result** section — displays the value with metadata
- **Copy** button — copies value to clipboard
- **Delete** button — deletes the key

**Tab: List**

```
Entity Type:  [String ▼]
Prefix:       [my-________]      (optional)

              [List]

Results:

my-config-key      String     2026-04-25T14:30:00Z   [View] [Delete]
my-other-key       JSON       2026-04-25T14:20:00Z   [View] [Delete]
my-secret          Binary     2026-04-25T14:10:00Z   [View] [Delete]
```

- **Prefix** — optional filter to list keys matching a prefix
- **Results** — table of keys with type, timestamp, and actions

**Tab: Delete**

```
Entity Type:  [String ▼]
Key:          [my-config-key________]

              [Confirm Delete]

⚠️  This action cannot be undone.
```

- **Confirm Delete** — removes the key permanently

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Store | Save | Stores or updates a key-value pair | `PUT /api/v1/kv/{type}/{key}` |
| Retrieve | Search | Fetches a value by key and type | `GET /api/v1/kv/{type}/{key}` |
| List | List | Lists all keys of a type (optionally filtered by prefix) | `GET /api/v1/kv/{type}` |
| Delete | Trash | Removes a key permanently | `DELETE /api/v1/kv/{type}/{key}` |

## Walkthrough

### Store and retrieve your first key-value pair

1. Open the KV Store view (`#/data/kv`).
2. Click the **Put** tab (should be default).
3. Set **Entity Type** to "String".
4. In **Key**, enter `greeting`.
5. In **Value**, enter `Hello, QuicBaaS!`.
6. Click **Store**.

**Expected outcome:**

```
✓ Tab resets (fields clear)
✓ Success toast: "Key stored: greeting"
```

7. Click the **Get** tab.
8. **Entity Type** should still be "String" (persists).
9. In **Key**, enter `greeting`.
10. Click **Search**.

**Expected outcome:**

```
✓ Result section appears:
  {
    "key": "greeting",
    "value": "Hello, QuicBaaS!",
    "type": "string",
    "created": "2026-04-25T14:35:00Z",
    "version": 1
  }
✓ [Copy] button is enabled
```

11. Click **Copy** to copy the value to clipboard. You can paste it elsewhere to verify.

### List and filter keys

12. Click the **List** tab.
13. **Entity Type** is still "String".
14. Leave **Prefix** empty and click **List**.

**Expected outcome:**

```
✓ Results table shows:
  greeting     String     2026-04-25T14:35:00Z   [View] [Delete]
```

15. Store a few more keys so we can test prefix filtering:
    - Click **Put** tab, store `greeting-es: Hola, QuicBaaS!`
    - Click **Put** tab, store `greeting-fr: Bonjour, QuicBaaS!`

16. Click **List** tab again.
17. In **Prefix**, enter `greeting-`.
18. Click **List**.

**Expected outcome:**

```
✓ Results show only keys starting with "greeting-":
  greeting-es       String     2026-04-25T14:36:00Z   [View] [Delete]
  greeting-fr       String     2026-04-25T14:37:00Z   [View] [Delete]
```

### Delete a key

19. Click the **Delete** tab.
20. **Entity Type** is "String".
21. In **Key**, enter `greeting-es`.
22. Click **Confirm Delete**.

**Expected outcome:**

```
✓ Tab resets
✓ Warning toast: "Key deleted: greeting-es"
✓ The key no longer appears in List results
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Store button is disabled | Key or value field is empty | Fill in all required fields |
| Search returns "Key not found" | Key does not exist or entity type is wrong | Check spelling; verify entity type matches the stored type |
| Delete confirms but key still appears in List | List cache is stale | Refresh the List tab or refresh the page |
| Entity Type dropdown shows only "String" | KV store is configured with limited types | Check daemon config; enable additional entity types |
| Large values are truncated in the Result section | Result section has a max display size | Use the API for large values: `curl https://localhost:18080/api/v1/kv/string/my-key` |

## See also

- [CLI: quicbaas kv](../cli/kv.md) — manage KV data via CLI
- [Data → Vectors](data-vectors.md) — vector search operations
- [API: database.md](../api/database.md) — KV API reference
