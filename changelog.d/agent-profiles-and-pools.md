### Added

- **Agent profiles and pools.** `AgentProfile` names one combination of
  *runtime* (claude / codex / opencode / openvino), *model* and *provider
  endpoint* — three independent facts that were previously fused into a single
  backend name. `AgentPool` is an ordered preference list of profiles, and
  `ProfileResolver` picks the first with capacity, so "prefer the local model,
  spill to the metered one" is data rather than a hardcoded fallback. Load
  comes from the same live-lease count the concurrency cap enforces, so
  placement and admission cannot disagree about what "busy" means.

### Removed

- **The `qwen` backend.** It was the codex runtime driving a qwen model at an
  ollama endpoint, registered under the model's name with the endpoint baked
  into its argv — so one codex registration could reach exactly one provider
  and a second provider needed a second "backend". It is now expressible as
  profiles over the runtimes that already exist. `QwenProviderName` and
  `QwenProviderConfigArgs` are superseded by `AgentProfile.SpawnArgs`.
