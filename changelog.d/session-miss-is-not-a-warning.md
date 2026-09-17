### Fixed

- **A routine session lookup logged up to six warnings.** The spawn server
  finds which backend owns a session by asking each one in turn until one
  answers, so every backend that does not hold it reports a miss — six of them
  on a daemon serving seven backends, for a lookup that then succeeds. At WARN
  that filled `agent.out` with `acp_backend: status session miss` for every
  healthy openvino and vllm turn, which is how a real fault becomes invisible.
  The diagnostic is unchanged and now emitted at debug; a miss is only
  interesting once every backend has missed, and the server already reports
  that as `NotFound`.
