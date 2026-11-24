# Playwright E2E suite

The suite runs against a real quicbaasd (`make test-e2e`, `make test-e2e-docker`).
The `chromium-no-backend` project runs every spec except session_lifecycle.spec.ts,
which the `chromium-acp` project runs and which needs an ACP backend.

## Coverage matrix

One row per route name in src/stores/router.svelte.ts; the vitest test
test/e2e-coverage-matrix.test.ts fails when a route has no row.

| Route | Hash | Flows | Specs |
| --- | --- | --- | --- |
| `login` | #/login | create, error state | bootstrap_activate.spec.ts, register.spec.ts |
| `register` | #/register | create, error state | register.spec.ts |
| `dashboard` | #/, #/dashboard | detail | dashboard_layout.spec.ts, light_theme_contrast.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, sidebar_nav.spec.ts, ssh_keys.spec.ts |
| `orchestration` | #/orchestration | detail | dashboard.spec.ts, dashboard_layout.spec.ts, sidebar_nav.spec.ts |
| `jobs` | #/jobs | list, create | dashboard.spec.ts, job_detail.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, sidebar_nav.spec.ts |
| `job-detail` | #/jobs/:id | detail, error state | job_detail.spec.ts |
| `fleet` | #/fleet | list | fleet_actions.spec.ts, fleet_disable.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, sidebar_nav.spec.ts, tenant_selector_layering.spec.ts |
| `fleet-node` | #/fleet/:id | detail, edit, delete | fleet_actions.spec.ts, fleet_disable.spec.ts |
| `sessions` | #/sessions | list, create | rapid_nav_no_duplicate_keys.spec.ts, session_attach.spec.ts, session_lifecycle.spec.ts, sidebar_nav.spec.ts |
| `session-detail` | #/sessions/:id | detail, delete | session_lifecycle.spec.ts |
| `tasks` | #/tasks | list, create | auto_spawn_task.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, sidebar_nav.spec.ts |
| `task-detail` | #/tasks/:id | detail, edit | none |
| `workflows` | #/workflows | list, create, edit, delete | rapid_nav_no_duplicate_keys.spec.ts, sidebar_nav.spec.ts, workflows.spec.ts, workflows_create.spec.ts |
| `events` | #/events, #/live-events | list | dashboard.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, sidebar_nav.spec.ts |
| `cost` | #/cost, #/cost-tracking | list | dashboard.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, sidebar_nav.spec.ts |
| `kv` | #/kv | list, create, edit, delete | data_surfaces.spec.ts, kv_roundtrip.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, tenant_selector_layering.spec.ts |
| `vectors` | #/vectors | list, create, delete, error state, namespace detail, index rebuild, compaction, export/import, status, hybrid and metadata search | data_surfaces.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, subsystem_gating.spec.ts |
| `timeseries` | #/timeseries | list, create, error state | data_surfaces.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, subsystem_gating.spec.ts |
| `objects` | #/objects | list, create, delete | bug_regressions.spec.ts, data_surfaces.spec.ts, rapid_nav_no_duplicate_keys.spec.ts |
| `rag` | #/rag | list, create, delete | ai_async_surfaces.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, renderer_freeze_reproducer.spec.ts |
| `embeddings` | #/embeddings | create | ai_async_surfaces.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, renderer_freeze_reproducer.spec.ts |
| `messaging` | #/messaging | list, create, delete | ai_async_surfaces.spec.ts, rapid_nav_no_duplicate_keys.spec.ts |
| `scheduler` | #/scheduler | list, create, delete | ai_async_surfaces.spec.ts, rapid_nav_no_duplicate_keys.spec.ts |
| `iam` | #/iam, #/iam/:tab | list, create, edit, delete | bug_regressions.spec.ts, iam_rbac.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, security_surfaces.spec.ts |
| `kms` | #/kms | list, create, delete | bug_regressions.spec.ts, kms_roundtrip.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, renderer_freeze_reproducer.spec.ts, security_surfaces.spec.ts |
| `secrets` | #/secrets | list, create, edit, delete | rapid_nav_no_duplicate_keys.spec.ts, secrets_roundtrip.spec.ts, security_surfaces.spec.ts |
| `certificates` | #/certificates | list, create, delete | none |
| `trust` | #/trust | list, create, delete | none |
| `crypto` | #/crypto | create | none |
| `dek` | #/dek | create | none |
| `dynamic-secrets` | #/dynamic-secrets | list, create, delete | none |
| `barrier` | #/barrier, #/seal | detail, edit, error state | barrier.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, security_surfaces.spec.ts |
| `tenant-barrier` | #/tenant-barrier | detail, edit | tenant_barrier.spec.ts |
| `ceremony` | #/ceremony | detail, create | ceremony.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, security_surfaces.spec.ts |
| `bootstrap` | #/bootstrap | detail, create | bootstrap.spec.ts, rapid_nav_no_duplicate_keys.spec.ts, security_surfaces.spec.ts |
| `ssh-keys` | #/ssh-keys, #/sshkeys | list, create, delete | rapid_nav_no_duplicate_keys.spec.ts, ssh_generate.spec.ts, ssh_keys.spec.ts |
| `my-sessions` | #/my-sessions | list, detail, delete | none |
| `attestation` | #/attestation | list, create, delete | none |
| `oauth-apps` | #/oauth-apps | list, create, edit, delete | none |
| `identity-providers` | #/identity-providers | list, create, edit, delete | none |
| `policy-tester` | #/policy-tester | detail | none |
| `impersonation` | #/impersonation | list, create, delete | none |
| `iam-audit` | #/iam-audit | list, detail, delete | none |
| `inference` | #/inference | list, create, error state | inference_no_provider.spec.ts, rapid_nav_no_duplicate_keys.spec.ts |
| `projects` | #/projects | list, create, edit, delete | projects.spec.ts, ref_picker_secret_create.spec.ts, ref_picker_ssh_key.spec.ts, sidebar_nav.spec.ts |
| `accounts` | #/accounts | list, create, delete | none |
| `prompts` | #/prompts | list, detail, edit, history, diff, rollback, reset | none |
| `attention` | #/attention | list | none |
| `cluster` | #/cluster | detail, leader, shard leader, rebalance plan and progress, migration start, progress and cancel | bug_regressions.spec.ts, light_theme_contrast.spec.ts |
| `nodes` | #/nodes | list, add, remove, replace, non-leader filter | nodes_view.spec.ts |
| `backup` | #/backup | list | backup_view.spec.ts |
| `connection` | #/connection | detail, edit, error state | connection_view.spec.ts |
| `invite-activate` | #/invite/:tenant/:token | detail, error state | bootstrap_activate.spec.ts |
| `not-found` | any unmatched hash | error state | bootstrap_activate.spec.ts |

## Not yet covered

- `task-detail`
- `certificates`
- `trust`
- `crypto`
- `dek`
- `dynamic-secrets`
- `accounts`
- `prompts`
- `attention`
- `my-sessions`
- `attestation`
- `oauth-apps`
- `identity-providers`
- `policy-tester`
- `impersonation`
- `iam-audit`

## Mocked specs

barrier.spec.ts, bootstrap.spec.ts, ceremony.spec.ts and nodes_view.spec.ts still
call page.route or route.fulfill; they are to be converted to run against the real
daemon.
