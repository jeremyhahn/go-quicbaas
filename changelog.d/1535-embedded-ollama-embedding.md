### Fixed

- **The embedded transport serves `quicbaas.Ollama` and `quicbaas.Embedding`**
  (#1535). Every Ollama client verb (Embed, EmbedWithModel, EmbedBatch,
  ListModels, GetModelInfo, Health, IsAvailable) and every Embedding client
  verb (GenerateEmbedding, BatchGenerateEmbeddings, ListProviders,
  GetProviderInfo, SetDefaultProvider, GetCacheStats, ClearCache) now reaches
  the service facade in process instead of failing with `UnknownMethodError`.
  When the facade has no provider, the call fails with
  `ServiceNotConfiguredError` naming `Ollama` or `Embedding`.
