# QuicBaaS GUI Walkthroughs

This section provides comprehensive, step-by-step walkthroughs for every view in the QuicBaaS web UI and Wails desktop app. Each page covers layout, actions, a complete example workflow, and troubleshooting.

## Orchestration

The orchestration subsystem manages stateless inference, sessions, jobs, agents, workflows, and event streams.

| View | Purpose |
|------|---------|
| [Dashboard](views/dashboard.md) | System health, uptime, service status, and resource counts |
| [Orchestrator → Jobs](views/orchestrator-jobs.md) | Queue, monitor, and cancel orchestration jobs |
| [Orchestrator → Agent Fleet](views/orchestrator-fleet.md) | Register, view, and decommission agent daemons |
| [Orchestrator → Sessions](views/orchestrator-sessions.md) | Spawn, prompt, attach, and close persistent sessions |
| [Orchestrator → Tasks](views/orchestrator-tasks.md) | Create, claim, complete, and decompose work items |
| [Orchestrator → Workflows](views/orchestrator-workflows.md) | Define, list, edit, duplicate, and run workflows |
| [Orchestrator → Live Events](views/orchestrator-events.md) | Stream job/session events in real time |
| [Orchestrator → Cost Tracking](views/orchestrator-cost.md) | Analyse per-job iteration usage and estimates |
| [Orchestrator → Inference](views/orchestrator-inference.md) | Submit stateless prompts to fleet-routed backends |

## Data

Storage, retrieval, and management of key-value, vector, time-series, and object data.

| View | Purpose |
|------|---------|
| [Data → KV Store](views/data-kv.md) | Entity-typed key-value put/get/delete operations |
| [Data → Vectors](views/data-vectors.md) | Vector search and embedding operations |
| [Data → Time-Series](views/data-timeseries.md) | Time-series metric ingestion and queries |
| [Data → Objects](views/data-objects.md) | Blob storage, tagging, and lifecycle operations |

## AI & ML

Retrieval-augmented generation, embeddings, and LLM integrations.

| View | Purpose |
|------|---------|
| [AI → RAG](views/ai-rag.md) | Ingest documents, query, manage namespaces |
| [AI → Embeddings](views/ai-embeddings.md) | Configure providers, set defaults, manage cache |

## Async

Messaging and scheduled job execution.

| View | Purpose |
|------|---------|
| [Async → Messaging](views/async-messaging.md) | Publish/subscribe channels and dedup policies |
| [Async → Scheduler](views/async-scheduler.md) | Cron jobs: create, run, pause, resume, delete |

## Security

Identity, access control, cryptography, and key management.

| View | Purpose |
|------|---------|
| [Security → IAM](views/security-iam.md) | Users, roles, policies; assign/revoke user roles |
| [Security → KMS](views/security-kms.md) | Create, rotate, and manage encryption keys |
| [Security → Secrets](views/security-secrets.md) | Store, rotate, and version secrets |
| [Security → Seal](views/security-seal.md) | Initialize, unseal, rekey, and rotate root key |
| [Security → SSH](views/security-ssh.md) | Generate, import keys; manage known hosts |

## Getting Started

New to QuicBaaS? Start here:

1. [Dashboard](views/dashboard.md) — check system health
2. [Agent Fleet](views/orchestrator-fleet.md) — register or view deployed agents
3. [Inference](views/orchestrator-inference.md) — submit your first prompt
4. [KMS](views/security-kms.md) — create an encryption key if needed

## Cross-Reference

- See the [CLI Reference](../cli/orchestrator.md) for command-line equivalents of each GUI action
- See [agents/inference.md](../agents/inference.md) for low-level architecture and API details
- See [security/](../security/) for in-depth encryption, IAM, and KMS design docs
