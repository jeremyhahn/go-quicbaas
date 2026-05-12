# Data → Vectors

## Purpose

The Vectors view manages vector embeddings and similarity search when the vector subsystem is enabled. Use this view to store vectors, search for similar vectors, and manage vector namespaces. Vectors are commonly used for semantic search, recommendations, and machine learning pipelines.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "Search", "Store", "Manage", "Settings"

**Tab: Search**

```
Namespace:    [default________]
Query Vector: [0.1, 0.2, 0.3, ...]
              (JSON array or paste from file)

Top-K:        [10]  (number of results)

              [Search]

Results:

Score   Vector ID        Metadata
─────────────────────────────────
0.987   vec_abc123       {"item":"apple"}
0.934   vec_def456       {"item":"apricot"}
0.812   vec_ghi789       {"item":"avocado"}
```

- **Namespace** — filter to a specific vector namespace (e.g., "products", "articles")
- **Query Vector** — paste a vector as a JSON array
- **Top-K** — limit number of results (default 10)
- **Results** — table of matching vectors sorted by similarity score (1.0 = exact match)

**Tab: Store**

```
Namespace:    [default________]
Vector ID:    [vec_my-item________]
Vector:       [0.1, 0.2, 0.3, ...]
Metadata:     [{"item":"apple"}________]
              (optional JSON object)

              [Clear] [Store]
```

- **Vector ID** — unique identifier for the vector
- **Vector** — JSON array of floats
- **Metadata** — optional JSON object for filtering and display

**Tab: Manage**

```
Namespace:    [default________]

              [List Vectors]

Vectors:

Vector ID           Metadata              Stored
─────────────────────────────────────────────────
vec_abc123          {"item":"apple"}      2026-04-25T14:30:00Z  [Delete]
vec_def456          {"item":"apricot"}    2026-04-25T14:35:00Z  [Delete]
vec_ghi789          {"item":"avocado"}    2026-04-25T14:40:00Z  [Delete]

Namespace Stats:
  Total vectors: 3
  Avg vector size: 1536 dimensions
```

- **List Vectors** — loads vectors in the namespace
- **Namespace Stats** — aggregate metadata

**Tab: Settings**

```
Vector Subsystem Status: ✓ Enabled

Embedding Provider: [OpenAI ▼]
Default Model:      [text-embedding-3-small]

Storage Backend:    PebbleDB
Index Type:         HNSW (Hierarchical Navigable Small World)

[Save Settings]
```

- Shows subsystem status and configuration
- Allows changing the default embedding provider

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Search Vectors | Search | Similarity search for k nearest neighbors | `POST /api/v1/vectors/search` |
| Store Vector | Save | Stores a vector with optional metadata | `POST /api/v1/vectors/store` |
| List Vectors | List | Lists all vectors in a namespace | `GET /api/v1/vectors/namespace/{namespace}` |
| Delete Vector | Trash | Removes a vector | `DELETE /api/v1/vectors/{vectorID}` |

## Walkthrough

### Store and search your first vector

1. Open the Vectors view (`#/data/vectors`).
2. Check the **Settings** tab to ensure the Vector subsystem is **Enabled**. If disabled, the view shows:

```
Vector subsystem is not enabled.
Enable it in the daemon configuration:
  [Enable] [View Docs]
```

3. Click the **Store** tab.
4. **Namespace** defaults to "default".
5. In **Vector ID**, enter `apple`.
6. In **Vector**, paste a sample embedding (e.g., a 1536-dimensional OpenAI embedding):

```json
[0.001234, 0.005678, ..., 0.002345]
```

7. In **Metadata**, optionally enter:

```json
{"item": "apple", "category": "fruit"}
```

8. Click **Store**.

**Expected outcome:**

```
✓ Tab resets
✓ Success toast: "Vector stored: apple"
```

9. Store a few more vectors for comparison:
   - Click **Store** tab, store `apricot` with a slightly different vector
   - Click **Store** tab, store `orange` with a different vector

10. Click the **Search** tab.
11. In **Query Vector**, paste the same vector you stored for `apple`:

```json
[0.001234, 0.005678, ..., 0.002345]
```

12. Leave **Top-K** as 10 (will return the 3 vectors you stored).
13. Click **Search**.

**Expected outcome:**

```
✓ Results table appears:
  Score   Vector ID        Metadata
  ──────────────────────────────────
  1.000   apple            {"item":"apple","category":"fruit"}
  0.892   apricot          {"item":"apricot","category":"fruit"}
  0.734   orange           {"item":"orange","category":"fruit"}
```

The first result has a perfect score (1.0) because it's identical to the query.

### Manage vectors

14. Click the **Manage** tab.
15. **Namespace** is "default".
16. Click **List Vectors**.

**Expected outcome:**

```
✓ Vectors table shows all 3 vectors
✓ Namespace Stats shows:
  Total vectors: 3
  Avg vector size: 1536 dimensions
```

17. To delete the `orange` vector, click its **[Delete]** button.
18. A confirm modal appears:

```
Delete vector orange?

This action cannot be undone.

[Cancel] [Delete]
```

19. Click **Delete**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Vector deleted: orange"
✓ orange no longer appears in the Manage list
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| "Vector subsystem is not enabled" message | Feature is disabled in daemon config | Enable vectors: `quicbaas config set vector.enabled true` |
| Store button is disabled | Required fields (Vector ID, Vector) are empty | Fill in all required fields |
| Search returns "No results" | Query vector is too different from stored vectors | Check that you have stored vectors; try a different query |
| Vector dimensions mismatch error | Query vector has different dimensions than stored vectors | Ensure all vectors use the same embedding model (e.g., all 1536-dim) |
| Namespace dropdown is empty | No vectors stored yet | Store at least one vector first |
| Large vector lists are slow to load | Too many vectors in namespace | Limit search to smaller date range; consider archiving old vectors |

## See also

- [CLI: quicbaas vectors](../cli/vectors.md) — manage vectors via CLI
- [vector/README.md](../vector/README.md) — vector architecture and configuration
- [AI → Embeddings](ai-embeddings.md) — embedding provider settings
- [Data → KV Store](data-kv.md) — entity-typed key-value storage
