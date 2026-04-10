# Tic-Tac-Toe Orchestration Example

Fully automated: starts a QuicBaaS server, runs the workflow, monitors progress, and cleans up on exit. Supports configurable GPU profiles and model families.

## Prerequisites

```bash
make build            # from repo root
ollama serve          # ollama must be running (models are auto-pulled)
opencode --version    # opencode installed (ACP backend for coder agent)
```

## Usage

```bash
./run.sh                                              # defaults: gpu-lite + llama
./run.sh --gpu-profile cpu                            # CPU only, tinyllama
./run.sh --gpu-profile gpu-power --model-family qwen  # RTX 4090 / A6000
./run.sh --gpu-profile gpu-max --model-family deepseek # RTX 6000 Pro
```

## GPU Profile + Model Family Matrix

| Tier | VRAM | Llama | Qwen | DeepSeek |
|------|------|-------|------|----------|
| cpu | 0 GB | tinyllama | tinyllama | tinyllama |
| gpu-lite | ~8 GB | llama3 | qwen2.5-coder:7b | deepseek-coder-v2:16b |
| gpu-power | ~48 GB | llama3.3:70b | qwen2.5-coder:72b | deepseek-coder-v2:236b |
| gpu-max | ~90 GB | llama3.3:70b | qwen2.5-coder:72b | deepseek-coder-v2:236b |

`gpu-max` uses the same models as `gpu-power` but with higher iteration limits (10 vs 5) and longer timeouts (60m vs 45m).

## VRAM Requirements

| Tier | VRAM Required | Example Hardware |
|------|---------------|------------------|
| cpu | 0 GB | Any CPU (slow inference) |
| gpu-lite | ~8 GB | RTX 3070, RTX 4060 Ti |
| gpu-power | ~48 GB | RTX 4090, A6000 |
| gpu-max | ~90 GB | RTX 6000 Pro, A100 80GB |

## Tier Settings

| Tier | Max Iterations | Timeout |
|------|---------------|---------|
| cpu | 5 | 30m |
| gpu-lite | 5 | 30m |
| gpu-power | 5 | 45m |
| gpu-max | 10 | 60m |

## Workflow Agents

| Role | Backend | Model | Purpose |
|------|---------|-------|---------|
| lead | ollama | (per matrix) | Task decomposition and planning |
| coder | opencode | anthropic:claude-sonnet | Code generation with persistent sessions |
| reviewer | ollama | (per matrix) | Output quality validation |

## Model Auto-Pull

The script checks `ollama list` for the required model before starting. If the model is not found locally, it runs `ollama pull` automatically. No manual model setup needed.

## GPU Detection

When a GPU tier is selected (anything except `cpu`), the script runs `nvidia-smi` to verify GPU availability. If no GPU is detected, it prints a warning but continues -- Ollama will fall back to CPU inference, which is significantly slower for larger models.

## Files

| File | Purpose |
|------|---------|
| `workflow-template.yaml` | Workflow template with placeholder variables |
| `run.sh` | Automated runner with GPU profile support |
| `data/` | Created at runtime -- server state and checkpoints |
| `server.log` | Server output log |
