# Orchestrator → Workflows

## Purpose

The Workflows view manages named, multi-step orchestration workflows. Use this view to list existing workflows, run them (with parameter entry), create new workflows (via an interactive wizard or advanced JSON editor), edit workflow definitions, duplicate for quick templating, and delete workflows. Workflows are reusable blueprints for complex orchestration logic.

## Layout

The page displays a searchable list of workflows with two main sections:

**Header** — "Workflows" title with "Multi-step orchestration templates" subtitle. Primary CTA is **+ New Workflow** button (splits into "Wizard" and "Advanced" sub-buttons).

**Search input** — client-side filter on workflow name and description.

**Cards or Table** — Each workflow shows:

- **Name** — workflow identifier
- **Description** — short summary (if provided)
- **Created** — ISO 8601 timestamp
- **Last Run** — timestamp or "Never"
- **Actions** — three-dot menu with: Run, Edit, Duplicate, Delete

**Empty State**:

```
No workflows.
Create your first workflow using the wizard or advanced JSON editor.

[+ Wizard] [+ Advanced]
```

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| New Workflow (Wizard) | Plus | Opens a step-by-step form to define a workflow interactively | `POST /api/v1/orchestrator/workflows` |
| New Workflow (Advanced) | Plus | Opens a raw JSON editor for power users | `POST /api/v1/orchestrator/workflows` |
| Run Workflow | Play | Opens a parameter-entry form; executes the workflow as a job | `POST /api/v1/orchestrator/jobs` (with workflow ref) |
| Edit Workflow | Edit | Opens the workflow JSON editor for inline modifications | `PUT /api/v1/orchestrator/workflows/{name}` |
| Duplicate Workflow | Copy | Creates a copy of the workflow with a new name | `POST /api/v1/orchestrator/workflows` (with copied JSON) |
| Delete Workflow | Trash | Removes the workflow definition | `DELETE /api/v1/orchestrator/workflows/{name}` |

## Walkthrough

### Create a workflow using the wizard

1. Open the Workflows view (`#/orchestration/workflows`).
2. Click **+ New Workflow** button and select **Wizard**.
3. A multi-step modal dialog opens:

**Step 1 — Basic Info**

```
Workflow Name:  [email-campaign-v1________]

Description:    [Send emails and track responses________]
                (optional)

[Next] [Cancel]
```

4. Enter a **Workflow Name** (no spaces; use hyphens or underscores).
5. Optionally enter a **Description**.
6. Click **Next**.

**Step 2 — Add Steps**

```
Steps:

[+ Add Step]

Step 1:  Type: [Prompt ▼]       Backend: [Claude ▼]
         Prompt: [user_email content__________]
         [Edit] [Delete]

[+ Add Step]

[Back] [Next] [Cancel]
```

7. Click **+ Add Step** to add the first step.
8. A sub-form appears:

```
Step Type:   [Prompt ▼]  (or Task, Inference, Wait, etc.)
Backend:     [Claude ▼]  (if Prompt selected)
Prompt:      [Who is the recipient?_____]
Host Affinity: [Any ▼]
[Save Step] [Cancel]
```

9. Fill in the step details. For a Prompt step, select Claude, enter a prompt.
10. Click **Save Step**.

**Expected outcome:**

```
✓ Step 1 appears in the Steps list
✓ Step summary shows: "Prompt to Claude: Who is the recipient?"
```

11. Click **+ Add Step** to add a second step (optional).
12. Add another step (e.g., "Wait 10 seconds" or "Check response").
13. When done adding steps, click **Next**.

**Step 3 — Outputs**

```
Workflow Output:

Result fields to capture:

[+ Add Output]

Output 1: [response_text________] Type: [String ▼]
          [Edit] [Delete]

[Back] [Create] [Cancel]
```

14. Optionally define output fields to capture results from steps.
15. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Workflow created"
✓ Workflow appears in the list
```

### Run the workflow

16. Click your new workflow's row.
17. Click the three-dot menu and select **Run Workflow**.
18. A parameter-entry dialog opens:

```
Run Workflow: email-campaign-v1

Parameters:

recipient_email: [user@example.com_________]
            
[Run] [Cancel]
```

19. Fill in any required parameters.
20. Click **Run**.

**Expected outcome:**

```
✓ Dialog closes
✓ Success toast: "Workflow job started"
✓ A new job appears in the Jobs view
```

### Edit and duplicate workflows

21. Click a workflow's three-dot menu and select **Edit**.
22. A JSON editor opens with the full workflow definition:

```json
{
  "name": "email-campaign-v1",
  "description": "Send emails...",
  "steps": [
    {
      "type": "prompt",
      "backend": "claude",
      "prompt": "Who is the recipient?"
    }
  ]
}
```

23. Modify the JSON as needed and click **Save**.
24. Alternatively, click **Duplicate** to create a copy with a new name (e.g., `email-campaign-v2`).

**Expected outcome:**

```
✓ Workflow is saved with changes
✓ Toast confirms: "Workflow updated"
✓ Duplicated workflow appears in the list
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Workflow name already exists | Another workflow has the same name | Choose a unique name; append `-v2`, `-prod`, etc. |
| Wizard form validation fails | Required fields are empty (name, at least one step) | Fill in all required fields before clicking Next |
| Run Workflow shows "Missing parameters" | Workflow expects input that you didn't provide | Check the workflow definition; provide all required parameters |
| JSON editor shows parse error | JSON syntax is invalid | Fix syntax errors (missing quotes, commas, braces) |
| Workflow step uses nonexistent backend | Backend is not registered in the fleet | Register the backend via Agent Fleet; edit the workflow to use an available backend |
| Delete Workflow is disabled | Workflow is currently running | Wait for the running job to complete or cancel it from the Jobs view |

## See also

- [CLI: quicbaas orchestrator workflows](../cli/orchestrator.md#quicbaas-orchestrator-workflows-listrun) — manage workflows via CLI
- [Orchestrator → Jobs](orchestrator-jobs.md) — monitor workflow execution as jobs
- [Orchestrator → Inference](orchestrator-inference.md) — single-step stateless prompts
- [configuration/workflow-settings.md](../configuration/workflow-settings.md) — advanced workflow configuration
