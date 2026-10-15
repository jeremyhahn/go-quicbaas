### Fixed

- **The SDK reaches the ParameterStore and Ollama verbs over MCP
  (go-quicbaas#185).** The SDK MCP transport derived tool names that the
  server never registered, so these calls failed with "tool not found".
  - ParameterStore `GetParameter`, `SetParameter`, `DeleteParameter`,
    `ListParameters`, `SearchByTags`, `GetParameters`, `SetParameters` and
    `DeleteParameters` now call `quicbaas_parameter_get`, `_set`, `_delete`,
    `_list`, `_search`, `_batch_get`, `_batch_set` and `_batch_delete`.
  - Ollama `ListModels`, `GetModelInfo` and `IsAvailable` now call
    `quicbaas_ollama_model_list`, `quicbaas_ollama_model_info` and
    `quicbaas_ollama_health`.
- **The transport translates the arguments and results of these verbs.**
  - Requests are sent in the string-typed arguments the tools parse.
  - Results are decoded into the typed `client/parameterstore` and
    `client/ollama` responses.
  - A wrong request type, or a parameter value that is not valid UTF-8, is
    refused as `InvalidInput` before any tool is called.
  - An unknown ParameterStore method is `Unimplemented`.
- **The Ollama tools return no `Timestamp` and no `Modelfile`.** Those
  fields stay zero over MCP.
- **The contract test no longer exempts these verbs.** `EmbedWithModel`
  stays listed.
