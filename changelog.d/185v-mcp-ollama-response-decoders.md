### Fixed

- **Ollama `Health`, `Embed` and `EmbedBatch` work over MCP
  (go-quicbaas#185).** The tool names already matched, but the SDK client
  rejected each tool result as the wrong type.
  - The SDK MCP transport now decodes the `quicbaas_ollama_health`,
    `quicbaas_ollama_embed` and `quicbaas_ollama_embed_batch` results into
    `HealthResponse`, `EmbedResponse` and `EmbedBatchResponse`.
  - `EmbedBatch` sends its texts as the JSON array string the tool expects.
  - A wrong request type is refused as `InvalidInput` before the tool is
    called.
- **The embed tools return no `Timestamp`.** It stays zero over MCP, as it
  does over REST.

### Removed

- **The SDK Ollama client no longer has `DefaultModel()` or `BaseURL()`.**
  No transport and no server verb served them, so they always returned an
  empty string. `HealthResponse.BaseURL` carries the base URL.
