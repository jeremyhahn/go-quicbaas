package mcp

import (
	"encoding/json"
	"testing"

	orchestrator "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestDecodeMCPListWorkflows_FullRESTShape(t *testing.T) {
	raw, err := json.Marshal(map[string]interface{}{
		"workflows": []map[string]interface{}{
			{
				"tenant_id":   "tenant-a",
				"project_id":  "project-a",
				"name":        "full-workflow",
				"description": "uses every workflow settings field",
				"mode":        "canned",
				"agents": []map[string]interface{}{
					{
						"role":             "lead",
						"backend":          "ollama",
						"model":            "llama3.3",
						"capabilities":     []string{"plan", "code"},
						"mcp_scopes":       []string{"orchestrator:task_read"},
						"count":            2,
						"workspace_path":   "/workspace",
						"permission_mode":  "default",
						"skip_permissions": true,
						"env":              map[string]string{"A": "B"},
						"sandboxed":        true,
						"container_image":  "agent:latest",
						"host_affinity":    "node-a",
						"num_ctx":          32768,
						"system_prompt":    "lead the workflow",
						"prompt_append":    "extra instructions",
					},
				},
				"settings": map[string]interface{}{
					"max_iterations":   11,
					"max_parallelism":  3,
					"poll_interval":    "2s",
					"timeout_per_item": "5m",
					"total_timeout":    "30m",
					"review_criteria": []map[string]interface{}{
						{
							"name":      "tests",
							"validator": "go-test",
							"config":    "pkg/...",
							"required":  true,
							"phase":     "reviewing",
						},
					},
					"topology":  "mesh",
					"isolation": "per_job",
					"peer_messaging": map[string]interface{}{
						"max_message_size": 4096,
						"max_inbox_size":   99,
						"overflow_action":  "drop_oldest",
						"inbox_ttl":        "1h",
					},
					"file_locking": map[string]interface{}{
						"lease_ttl":          "10s",
						"heartbeat_interval": "1s",
						"max_renewals":       7,
					},
					"vector_db": map[string]interface{}{
						"enabled":         true,
						"index_type":      "hnsw",
						"auto_index":      true,
						"triggers":        []string{"artifact", "message"},
						"gpu_accelerated": true,
					},
					"cost_controls": map[string]interface{}{
						"max_tokens_per_job":   1000,
						"max_tokens_per_agent": 250,
						"budget_action":        "pause",
					},
					"recovery": map[string]interface{}{
						"auto_restart":    true,
						"max_restarts":    4,
						"restart_backoff": "3s",
					},
					"verbosity": "debug",
					"num_ctx":   65536,
					"hooks":     "standard",
				},
			},
		},
		"count":       1,
		"total":       1,
		"limit":       25,
		"offset":      0,
		"next_offset": 25,
	})
	if err != nil {
		t.Fatalf("marshal MCP workflow result: %v", err)
	}

	decoded, err := decodeMCPListWorkflows(raw)
	if err != nil {
		t.Fatalf("decodeMCPListWorkflows: %v", err)
	}
	resp, ok := decoded.(*orchestrator.ListWorkflowsResponse)
	if !ok {
		t.Fatalf("decoded response = %T, want *orchestrator.ListWorkflowsResponse", decoded)
	}
	if len(resp.Workflows) != 1 {
		t.Fatalf("workflow count = %d, want 1", len(resp.Workflows))
	}

	workflow := resp.Workflows[0]
	if workflow.Agents[0].SystemPrompt != "lead the workflow" ||
		workflow.Agents[0].MCPScopes[0] != "orchestrator:task_read" ||
		workflow.Agents[0].NumCtx != 32768 {
		t.Fatalf("agent role did not preserve REST fields: %+v", workflow.Agents[0])
	}
	settings := workflow.Settings
	if settings.MaxIterations != 11 ||
		settings.MaxParallelism != 3 ||
		settings.PollInterval != "2s" ||
		settings.TimeoutPerItem != "5m" ||
		settings.TotalTimeout != "30m" ||
		settings.ReviewCriteria[0].Phase != "reviewing" ||
		settings.PeerMessaging.MaxInboxSize != 99 ||
		settings.FileLocking.MaxRenewals != 7 ||
		settings.VectorDB.Triggers[1] != "message" ||
		settings.CostControls.BudgetAction != "pause" ||
		settings.Recovery.MaxRestarts != 4 ||
		settings.NumCtx != 65536 ||
		settings.Hooks != "standard" {
		t.Fatalf("settings did not preserve REST fields: %+v", settings)
	}
}
