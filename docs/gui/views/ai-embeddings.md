# AI → Embeddings

## Purpose

The Embeddings view configures embedding providers (OpenAI, local models, etc.), sets a system default, enables or disables providers, and monitors embedding cache statistics. Use this view to manage which LLM embedders are available for RAG, vector search, and semantic operations.

## Layout

The page displays a list of embedding providers with configuration and status:

**Header** — "Embeddings" title with "Configure embedding providers" subtitle. Controls:

- **Add Provider** button — registers a new embedding provider
- **Default Provider indicator** — shows which provider is current default (marked with a star or badge)

**Providers List** — Table or card grid:

```
Provider                   Status      Model               Enabled  Default   Actions
────────────────────────────────────────────────────────────────────────────────────
OpenAI                     ✓ OK        text-embedding-3-s  ✓        ★         [⋮]
Local (Ollama)             ✓ OK        nomic-embed-text    ✓        —         [⋮]
Cohere                     ⚠ Offline   embed-english-v3    ✗        —         [⋮]

Cache Statistics:
  Size: 1.2 GB / 5 GB (24%)
  Entries: 12,450
  Hit Rate: 87.3%

[Clear Cache]
```

- **Status** — OK (green), Offline (gray), Error (red)
- **Model** — embedding model identifier
- **Enabled** — toggle to enable/disable provider
- **Default** — star icon for the current default
- **Cache stats** — shared across all providers

**Provider Details Modal** (on click):

```
Provider: OpenAI

API Endpoint:   https://api.openai.com/v1/embeddings
API Key:        [••••••••••••••••••] (masked)
Model:          text-embedding-3-small
Dimensions:     1536
Max Input Length: 8000 tokens

Last Test:      2026-04-25T14:30:00Z (success)

[Test Connection] [Set as Default] [Edit] [Delete] [Close]
```

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Add Provider | Plus | Opens a form to register a new embedding provider | `POST /api/v1/embeddings/providers` |
| Toggle Enable | Switch | Enables or disables a provider | `POST /api/v1/embeddings/providers/{name}/status` |
| Set Default | Star | Marks a provider as the system default | `PUT /api/v1/embeddings/providers/{name}/default` |
| Test Connection | Play | Validates provider connectivity and API key | (implicit in UI; no dedicated endpoint) |
| Edit Provider | Edit | Modifies provider configuration (API key, model, etc.) | `PUT /api/v1/embeddings/providers/{name}` |
| Delete Provider | Trash | Removes a provider; shows confirm modal if it is the default | `DELETE /api/v1/embeddings/providers/{name}` |
| Clear Cache | Trash | Flushes the embedding cache to free memory | (client-side; clearing cache may invoke `POST /api/v1/embeddings/cache/clear`) |

## Walkthrough

### Add a new embedding provider

1. Open the Embeddings view (`#/ai/embeddings`).
2. Click **+ Add Provider** button.
3. A form modal appears:

```
Register Embedding Provider

Name:               [OpenAI_____]
Type:               [OpenAI ▼]  (or Cohere, Ollama, Local, HuggingFace)
API Endpoint:       [https://api.openai.com/v1/embeddings]
API Key / Token:    [sk-proj-••••••••______________]
Model:              [text-embedding-3-small ▼]

Advanced:
  Dimensions:       [1536]  (auto-detected)
  Max Input Tokens: [8000]  (auto-detected)
  Batch Size:       [100]

                    [Test Connection] [Register] [Cancel]
```

4. Fill in the provider details:
   - **Name**: `openai-prod`
   - **Type**: OpenAI
   - **API Endpoint**: (auto-filled for OpenAI)
   - **API Key**: Enter your OpenAI API key
   - **Model**: `text-embedding-3-small`

5. Click **Test Connection** to verify the API key works.

**Expected outcome:**

```
✓ Success message: "Connection test passed. Model supports 1536 dimensions."
✓ Dimensions and Max Input Tokens auto-populate
```

6. Click **Register**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Provider registered: openai-prod"
✓ New provider appears in the list with status ✓ OK
```

### Set a default provider

7. The new provider appears in the list. To set it as the default, click its row's three-dot menu.
8. Select **Set as Default**.

**Expected outcome:**

```
✓ Success toast: "Default provider changed to openai-prod"
✓ Star icon appears next to the provider name
✓ Other providers' stars disappear
```

9. Verify the change by checking the **Default Provider** indicator at the top of the page.

### Enable/disable a provider

10. Click a provider's **Enabled** toggle to disable it.

**Expected outcome:**

```
✓ Toggle switches off
✓ Provider status grays out
✓ The provider is no longer available for new embedding operations
✓ Existing cached embeddings remain (toggle re-enables them without recalculation)
```

11. Toggle it back on to re-enable.

### Manage cache

12. Scroll down to see **Cache Statistics**:

```
Cache Statistics:
  Size: 1.2 GB / 5 GB (24%)
  Entries: 12,450
  Hit Rate: 87.3%
```

13. Click **Clear Cache** to free memory (useful if cache is full or you want to force re-embedding).

**Expected outcome:**

```
✓ Confirm modal appears:
  "Clear the embedding cache? This will free ~1.2 GB. New embeddings will be 
   recalculated on next use."
  [Cancel] [Clear]
  
✓ After clearing:
  Success toast: "Cache cleared"
  Cache size resets to 0 GB / 5 GB
  Hit rate resets
```

### Edit and delete providers

14. Click a provider's three-dot menu and select **Edit**.
15. A form pre-fills with the provider's current settings.
16. Modify the API key or model (e.g., switch to `text-embedding-3-large`).
17. Click **Save**.

**Expected outcome:**

```
✓ Success toast: "Provider updated: openai-prod"
✓ Provider details in the list update
```

18. To delete a provider, click its three-dot menu and select **Delete**.
19. If it is the default, a warning appears:

```
Delete default provider?

openai-prod is currently the default. If deleted, the system will 
select a new default automatically.

[Cancel] [Delete Anyway]
```

20. Click **Delete Anyway**.

**Expected outcome:**

```
✓ Success toast: "Provider deleted: openai-prod"
✓ Provider disappears from the list
✓ Default switches to another provider (or "None" if all are deleted)
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Register button is disabled | API Key or required fields are empty | Fill in all required fields |
| Test Connection fails with "401 Unauthorized" | API key is invalid or expired | Check your API key; regenerate if needed |
| Test Connection fails with "Network error" | Provider endpoint is unreachable | Verify endpoint URL; check network connectivity; verify firewall rules |
| Cannot set a provider as default | Provider is disabled (Enabled toggle is off) | Enable the provider first by toggling it on |
| Cannot delete the only provider | System requires at least one provider | Add a new provider first; then delete the old one |
| Cache clear is slow | Cache is very large (>10 GB) | Wait; this is normal for large caches |
| Embedding operations still use old model after switching providers | Cache has old embeddings cached | Clear the cache after changing the default provider |

## See also

- [CLI: quicbaas embeddings](../cli/embeddings.md) — manage providers via CLI
- [embedding/providers.md](../embedding/providers.md) — supported embedding providers and configuration
- [embedding/README.md](../embedding/README.md) — embedding architecture
- [AI → RAG](ai-rag.md) — RAG document ingestion and search
- [Data → Vectors](data-vectors.md) — vector storage and similarity search
