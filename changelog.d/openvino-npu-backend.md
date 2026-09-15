### Added

- **An `openvino` agent backend, and a container that can drive an Intel NPU.**
  `pkg/orchestration/openvino_backend.go` runs a real agent loop against an
  OpenVINO Model Server — completion, tool calls executed in the session's
  container, results fed back — offering exactly one tool, `run_command`.
  `deployments/inference/openvino-npu/Dockerfile` adds the Intel NPU user-mode
  driver to `openvino/model_server`, which ships the NPU plugin but no
  level-zero and therefore cannot enumerate the device at all. Measured on the
  burn-in fleet with `Qwen3-8B-int4-cw-ov`: 98% NPU utilisation with the GPU at
  0%, so reading work stops competing with reviews for the card. The agent
  daemon registers it from `--openvino-url` / `--openvino-model`; unset leaves
  it unregistered rather than pointing at a guess.

  It is a backend rather than another provider config on the ACP exec path
  because that was tried first and cannot work: OVMS answers *"Only function
  tools are supported"* to codex, which advertises its shell and patch tools as
  custom types and offers no switch to change them.
