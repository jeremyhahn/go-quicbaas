# Orchestrator → Tasks

## Purpose

The Tasks view displays work items created by orchestration jobs. Use this view to create, claim, complete, and decompose tasks. Tasks are typically created programmatically by workflows or jobs, but the GUI allows manual creation for testing and one-off work items.

## Layout

The page displays a searchable, filterable list of tasks:

**Header** — "Tasks" title with "Work items and task queue" subtitle. Primary CTA is **+ Create Task** button.

**Filters and Search** — Above the table:

- **Status filter** — "All", "Pending", "Claimed", "Completed", "Failed". Defaults to "All".
- **Search input** — client-side filter on task ID and description.

**Table** — Columns:

- **Task ID** — truncated UUID (e.g., `tsk_abc123…`)
- **Description** — user-provided description or empty
- **Status** — badge: "pending" (gray), "claimed" (blue), "completed" (green), "failed" (red)
- **Created** — ISO 8601 timestamp
- **Claimed By** — agent name or "—" if not yet claimed
- **Actions** — three-dot menu with: Claim, Complete, Decompose, Delete

**Empty State**:

```
No tasks.
Create a new task or run a workflow.

[+ Create Task button]
```

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create Task | Plus | Opens a modal to create a new task manually | `POST /api/v1/orchestrator/tasks` |
| Claim Task | Check | Claim a pending task for this agent; shows a confirm modal | `POST /api/v1/orchestrator/tasks/{taskID}/claim` |
| Complete Task | CheckDouble | Mark a claimed task as complete | `POST /api/v1/orchestrator/tasks/{taskID}/complete` |
| Decompose Task | GitBranch | Break a task into subtasks; opens a modal with JSON editor | `POST /api/v1/orchestrator/tasks/decompose` |
| Delete Task | Trash | Remove a task (pending tasks only) | `DELETE /api/v1/orchestrator/tasks/{taskID}` |

## Walkthrough

### Create and complete your first task

1. Open the Tasks view (`#/orchestration/tasks`).
2. Click **+ Create Task** button.
3. A modal form appears:

```
Create Task

Description: [_________________________________]
             (e.g., "Review PR #1234")

Metadata:    [JSON editor or key-value pairs]
             (optional, for task data)

[Cancel] [Create]
```

4. Enter a description (e.g., "Review code changes").
5. Optionally add metadata (key-value pairs or JSON).
6. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Task created"
✓ New task appears in the table with status "pending" (gray)
✓ Task ID is displayed
```

7. Find your new task in the table.
8. Click the row's three-dot menu and select **Claim Task**.
9. A confirm modal appears:

```
Claim this task?

Task will be marked as claimed and assigned to your agent.

[Cancel] [Claim Task]
```

10. Click **Claim Task**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Task claimed"
✓ Task status changes to "claimed" (blue)
✓ "Claimed By" field shows your agent name
```

11. After processing the task, click the row's three-dot menu and select **Complete Task**.
12. Another confirm modal appears:

```
Complete this task?

Task will be marked as completed. This cannot be undone.

[Cancel] [Complete Task]
```

13. Click **Complete Task**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Task completed"
✓ Task status changes to "completed" (green)
```

14. Optional: Try decomposing a task. Click a task's row three-dot menu and select **Decompose Task**.
15. A modal with a JSON editor opens:

```
Decompose Task

Input:   [{"subtask": "Part A", "priority": "high"}]

Output:  [{"id": "tsk_xxx", "state": "pending"}]

[Cancel] [Decompose]
```

16. Modify the JSON to define subtasks and click **Decompose**.

**Expected outcome:**

```
✓ Modal closes
✓ New subtasks appear in the task list
✓ Parent task is marked as decomposed
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create Task form validation error | Description is empty | Enter a task description |
| Claim Task button is disabled | Task is already claimed by another agent | Complete the task first or wait for it to be released |
| Complete Task fails | Task is not in "claimed" state | Claim the task first via the Claim action |
| Decompose Task shows JSON parse error | JSON syntax is invalid | Fix the JSON and try again |
| Task does not appear after creation | API response was lost (network timeout) | Refresh the page to see the task; check daemon logs |

## See also

- [CLI: quicbaas orchestrator tasks](../cli/orchestrator.md) — manage tasks via CLI
- [Orchestrator → Jobs](orchestrator-jobs.md) — jobs that generate tasks
- [Orchestrator → Workflows](orchestrator-workflows.md) — workflows that create tasks automatically
