# Orchestration Workflows

Workflow definitions control how the orchestration engine decomposes tasks, assigns agents, and reviews results.

## Workflow YAML Format

```yaml
name: <string>              # Unique workflow identifier
description: <string>       # Human-readable description
mode: <canned|constrained|autonomous>
agents:
  - role: <string>          # Agent role: lead, coder, reviewer, etc.
    backend: <string>       # Backend name: claude, codex, ollama
    model: <string>         # Model identifier
    capabilities: [<string>]  # persistent_sessions, tool_use, file_access
    mcp_scopes: [<string>]  # IAM scopes: orchestrator:read, kms:read, etc.
    count: <int>            # Optional: number of agents with this role (default: 1)
settings:
  max_iterations: <int>    # Max re-delegation cycles before escalation
  max_parallelism: <int>   # Max concurrent work items
  poll_interval: <duration> # Status poll interval (e.g., 5s)
  timeout_per_item: <duration>  # Max time per work item
  total_timeout: <duration>     # Max total job time
  review_criteria:
    - name: <string>        # Criterion name
      validator: <string>   # agent, command, or policy
      config: <string>      # Validator-specific config
      required: <bool>      # Whether failure blocks completion
```

## Workflow Modes

### Canned

Fully predetermined workflow with fixed steps. The lead agent does not decompose tasks; work items are predefined. Best for repeatable pipelines like CI/CD or standard reviews.

### Constrained

The lead agent decomposes tasks into work items, but operates within the agent roles and capabilities defined in the workflow YAML. The engine enforces role boundaries, parallelism limits, and review criteria. Best for structured development tasks.

### Autonomous

The lead agent has full control over task decomposition, agent spawning, and execution strategy. Minimal guardrails beyond iteration limits and timeouts. Best for exploratory or complex tasks where the optimal approach is unknown.

## Built-in Workflows

### software-dev (constrained)

Full software development pipeline with lead architect, coder, and QA reviewer.

```yaml
name: software-dev
description: "Full software development with lead architect, coder, and QA reviewer"
mode: constrained
agents:
  - role: lead
    backend: claude
    model: claude-sonnet-4-5-20250514
    capabilities: [persistent_sessions, tool_use]
    mcp_scopes: [orchestrator:read, orchestrator:write, agent:spawn, agent:prompt, agent:close]
  - role: coder
    backend: claude
    model: claude-sonnet-4-5-20250514
    capabilities: [persistent_sessions, tool_use, file_access]
    mcp_scopes: [kms:read, secrets:read, orchestrator:read]
  - role: reviewer
    backend: codex
    model: o4-mini
    capabilities: [persistent_sessions, tool_use]
    mcp_scopes: [orchestrator:read]
settings:
  max_iterations: 10
  max_parallelism: 2
  poll_interval: 5s
  total_timeout: 30m
```

### code-review (constrained)

Lightweight code review workflow with Ollama lead and Codex reviewer.

```yaml
name: code-review
description: "Code review and QA workflow"
mode: constrained
agents:
  - role: lead
    backend: ollama
    model: llama3
    capabilities: []
  - role: reviewer
    backend: codex
    model: o4-mini
    capabilities: [persistent_sessions, tool_use]
    mcp_scopes: [orchestrator:read]
settings:
  max_iterations: 3
  poll_interval: 5s
  total_timeout: 15m
```

### autonomous

Lead agent manages everything with broad permissions.

```yaml
name: autonomous
description: "Fully autonomous - lead agent manages everything"
mode: autonomous
agents:
  - role: lead
    backend: claude
    model: claude-sonnet-4-5-20250514
    capabilities: [persistent_sessions, tool_use]
    mcp_scopes: [orchestrator:read, orchestrator:write, agent:spawn, agent:prompt, agent:close, agent:admin]
settings:
  max_iterations: 20
  max_parallelism: 4
  poll_interval: 5s
  total_timeout: 60m
```

## CLI Usage

```bash
# Run with a built-in workflow
quicbaas orchestrator run "Build authentication service" --workflow software-dev

# Run with a custom workflow file
quicbaas orchestrator run "Review PR #42" --workflow-file ./my-review.yaml

# Run with defaults (no workflow specified)
quicbaas orchestrator run "Build a REST API" --backend ollama

# List available workflows
quicbaas orchestrator workflow list
quicbaas orchestrator workflow list --output json

# Show workflow details
quicbaas orchestrator workflow show software-dev
quicbaas orchestrator workflow show code-review --output yaml
```

## GPU Profiles

GPU profiles allow workflows to select Ollama models based on available hardware. The tictactoe example demonstrates this with a tier + model-family matrix.

### Model Matrix

| Tier | VRAM | llama | qwen | deepseek |
|------|------|-------|------|----------|
| `cpu` | 0 GB | tinyllama | tinyllama | tinyllama |
| `gpu-lite` | ~8 GB | llama3 | qwen2.5-coder:7b | deepseek-coder-v2:16b |
| `gpu-power` | ~48 GB | llama3.3:70b | qwen2.5-coder:72b | deepseek-coder-v2:236b |
| `gpu-max` | ~90 GB | llama3.3:70b | qwen2.5-coder:72b | deepseek-coder-v2:236b |

`gpu-max` uses the same models as `gpu-power` but with higher iteration limits (10 vs 5) and extended timeouts (60m vs 45m).

### Template-Based Generation

The tictactoe example uses a workflow template (`workflow-template.yaml`) with placeholders that are resolved at runtime based on the selected profile:

```yaml
name: tictactoe-{{PROFILE}}
description: "Tic-tac-toe game development ({{PROFILE_DESC}})"
mode: constrained
agents:
  - role: lead
    backend: ollama
    model: {{OLLAMA_MODEL}}
  - role: coder
    backend: opencode
    model: anthropic:claude-sonnet-4-5-20250514
    capabilities: [persistent_sessions, tool_use, file_access]
    mcp_scopes: [orchestrator:read]
  - role: reviewer
    backend: ollama
    model: {{OLLAMA_MODEL}}
settings:
  max_iterations: {{MAX_ITERATIONS}}
  max_parallelism: 1
  poll_interval: 5s
  total_timeout: {{TOTAL_TIMEOUT}}
```

The `run.sh` script resolves these placeholders using `--gpu-profile` and `--model-family` flags:

```bash
./run.sh --gpu-profile gpu-lite --model-family llama
./run.sh --gpu-profile gpu-power --model-family qwen
```

### Tier Settings

| Tier | Max Iterations | Total Timeout |
|------|---------------|---------------|
| `cpu` | 5 | 30m |
| `gpu-lite` | 5 | 30m |
| `gpu-power` | 5 | 45m |
| `gpu-max` | 10 | 60m |

## Creating Custom Workflows

1. Create a YAML file following the format above.
2. Place it in the `workflows/` directory or reference it with `--workflow-file`.
3. Define agent roles matching your team structure.
4. Set `mcp_scopes` for each role to control which MCP tools agents can access.
5. Configure `settings` for iteration limits, parallelism, and timeouts.

Validation rules:
- `name` is required and must be unique.
- `mode` must be `canned`, `constrained`, or `autonomous`.
- At least one agent must be defined.
- `canned` and `constrained` modes require a `lead` role agent.
- `mcp_scopes` follow the `resource:action` format (e.g., `kms:read`, `secrets:write`).
