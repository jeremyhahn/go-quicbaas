### Fixed

- **The inference and embedding upstream endpoints resolve through
  configuration, not through literals in the code (go-quicbaas#185).**
  `pkg/config/upstream_defaults.go` now states each provider's default
  endpoint once: Ollama, OpenAI, Voyage and LM Studio. The RAG embedding
  factory, the server's embedding wiring, the embedded fleet node and the
  `--embedded-fleet-node-ollama-url` flag default all read that value instead
  of restating it.
- **`rag.embedding.api_url` can be set from the environment** as
  `QUICBAAS_RAG_EMBEDDING_API_URL`, and the environment value wins over the
  config file.
- **With RAG unconfigured, the server's embedding manager dials
  `ollama.base_url`.** It used to fall back to a hard-coded local address
  whenever Ollama was disabled.
- **An endpoint that resolves empty is a typed
  `config.UpstreamEndpointMissingError` naming the key to set.**
  - For a provider with no default, embedding stays disabled.
  - For a blank `server.embedded_fleet_node.ollama_url`, the embedded fleet
    node refuses to start.
- **The embedded fleet node's default Ollama endpoint is now
  `http://localhost:11434`**, the same value as `ollama.base_url`. It was
  previously `http://127.0.0.1:11434`.
