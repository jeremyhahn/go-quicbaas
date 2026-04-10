# Calculator Orchestration Example

Fully local orchestration: all agents use Ollama for LLM inference via OpenCode. Zero API costs — everything runs on your GPU.

## Architecture

| Role     | Backend  | LLM          | Purpose                   |
|----------|----------|--------------|---------------------------|
| lead     | ollama   | (per profile)| Task decomposition        |
| coder    | opencode | ollama       | Code generation (local)   |
| reviewer | opencode | ollama       | Code review (local)       |

OpenCode agents connect to Ollama's OpenAI-compatible API (`localhost:11434/v1`), so all inference stays on your GPU.

## GPU Profile Matrix

| Tier | VRAM | Llama | Qwen | DeepSeek |
|------|------|-------|------|----------|
| cpu | 0 GB | tinyllama | tinyllama | tinyllama |
| gpu-lite | ~8 GB | llama3 | qwen2.5-coder:7b | deepseek-coder-v2:16b |
| gpu-power | ~48 GB | llama3.3:70b | qwen2.5-coder:72b | deepseek-r1:70b |
| gpu-max | ~90 GB | llama3.3:70b | qwen2.5-coder:72b | deepseek-r1:70b |

## Usage

```bash
./run.sh                                              # gpu-lite + llama (default)
./run.sh --gpu-profile cpu                            # CPU only, fast iteration
./run.sh --gpu-profile gpu-power --model-family qwen  # 72B on GPU
./run.sh --gpu-profile gpu-max --model-family deepseek # RTX 6000 Pro
```

## Prerequisites

```bash
make build                                             # QuicBaaS binaries
ollama serve                                           # Ollama running (models auto-pulled)
go install github.com/opencode-ai/opencode@latest     # OpenCode agent
```

## Output

Generated code is written to `output/` directory.

## Files

| File | Purpose |
|------|---------|
| `workflow-template.yaml` | Workflow template with profile placeholders |
| `run.sh` | Automated runner with GPU profile support |
| `output/` | Generated calculator project (created at runtime) |
| `data/` | Server state (created at runtime) |
| `server.log` | Server output with audit trail |
