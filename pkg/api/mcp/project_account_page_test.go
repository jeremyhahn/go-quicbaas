//go:build mcp

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package mcp

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// Fixtures
// =============================================================================

// mcpPagedProjects builds n projects, oldest first.
func mcpPagedProjects(n int) []*orchestration.Project {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	projects := make([]*orchestration.Project, 0, n)
	for i := range n {
		projects = append(projects, &orchestration.Project{
			ID:        "proj-" + strconv.Itoa(i),
			TenantID:  "tenant-1",
			Name:      "service-" + strconv.Itoa(i),
			CreatedAt: created.Add(time.Duration(i) * time.Hour),
		})
	}
	return projects
}

// mcpPagedProjectService answers the listing by running the REAL page helper
// over canned projects, exactly as the project service does over its store,
// and records the request the tool built.
func mcpPagedProjectService(projects []*orchestration.Project, captured **service.ListProjectsRequest) *mockProjectService {
	return &mockProjectService{
		listFunc: func(_ context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
			if captured != nil {
				*captured = req
			}
			page, err := orchestration.ApplyPage(projects, req.Page, orchestration.ProjectPageSpec())
			if err != nil {
				return nil, err
			}
			return &service.ListProjectsResponse{
				Projects:   page.Items,
				Total:      page.Total,
				Limit:      page.Limit,
				Offset:     page.Offset,
				NextOffset: page.NextOffset,
				Timestamp:  time.Now().UTC(),
			}, nil
		},
	}
}

// mcpPagedAccounts builds n accounts, oldest first.
func mcpPagedAccounts(n int) []*orchestration.Account {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	accounts := make([]*orchestration.Account, 0, n)
	for i := range n {
		accounts = append(accounts, &orchestration.Account{
			ID:          "acct-" + strconv.Itoa(i),
			TenantID:    "tenant-1",
			DisplayName: "alice-" + strconv.Itoa(i),
			Provider:    orchestration.AccountProviderClaude,
			Status:      orchestration.AccountStatusActive,
			CreatedAt:   created.Add(time.Duration(i) * time.Hour),
		})
	}
	return accounts
}

// mcpPagedAccountService pages canned accounts with the real page helper and
// records the request the tool built.
func mcpPagedAccountService(accounts []*orchestration.Account, captured **service.ListAccountsRequest) *mocks.MockAccountService {
	return &mocks.MockAccountService{
		ListAccountsFunc: func(_ context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			if captured != nil {
				*captured = req
			}
			page, err := orchestration.ApplyPage(accounts, req.Page, orchestration.AccountPageSpec())
			if err != nil {
				return nil, err
			}
			return &service.ListAccountsResponse{
				Accounts:   page.Items,
				Total:      page.Total,
				Limit:      page.Limit,
				Offset:     page.Offset,
				NextOffset: page.NextOffset,
				Timestamp:  time.Now().UTC(),
			}, nil
		},
	}
}

// decodedToolResult asserts a tool result is a JSON object.
func decodedToolResult(t *testing.T, result interface{}) map[string]interface{} {
	t.Helper()

	decoded, ok := result.(map[string]interface{})
	require.True(t, ok, "tool result is %T, want a JSON object", result)
	return decoded
}

// pageItemIDs returns the records under itemsKey with the ID of each.
func pageItemIDs(t *testing.T, decoded map[string]interface{}, itemsKey string) []string {
	t.Helper()

	items, ok := decoded[itemsKey].([]interface{})
	require.True(t, ok, "%s is %T, want a list", itemsKey, decoded[itemsKey])
	ids := make([]string, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]interface{})
		require.True(t, ok, "record is %T, want an object", item)
		id, _ := record["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// toolSchemaProperties returns the input-schema properties of a registered tool.
func toolSchemaProperties(t *testing.T, reg *ToolRegistry, name string) map[string]interface{} {
	t.Helper()

	for _, tool := range reg.ListTools() {
		if tool.Name != name {
			continue
		}
		properties, ok := tool.InputSchema["properties"].(map[string]interface{})
		require.True(t, ok, "properties is %T, want a map", tool.InputSchema["properties"])
		require.Contains(t, tool.Description, "25", "the description must document the default page size")
		return properties
	}
	t.Fatalf("tool %s is not registered", name)
	return nil
}

// =============================================================================
// project_list
// =============================================================================

func listProjectsTool(t *testing.T, svc service.ProjectService, args map[string]interface{}) map[string]interface{} {
	t.Helper()

	reg := newProjectAdminRegistry(t, svc, nil)
	result, err := reg.executeProjectTool(context.Background(), "project_list", args)
	require.NoError(t, err)
	return decodedToolResult(t, result)
}

// TestMCPProjectList_DefaultPageAndOffsetWalk proves an argument-free call
// answers with the default page of 25 and the matching total rather than the
// whole registry, and that offset walks to the last page.
func TestMCPProjectList_DefaultPageAndOffsetWalk(t *testing.T) {
	var captured *service.ListProjectsRequest
	svc := mcpPagedProjectService(mcpPagedProjects(30), &captured)

	first := listProjectsTool(t, svc, map[string]interface{}{"tenant_id": "tenant-1"})

	require.NotNil(t, captured.Page, "the tool must pass a non-nil page")
	ids := pageItemIDs(t, first, "projects")
	require.Len(t, ids, 25)
	require.Equal(t, "proj-29", ids[0], "newest first")
	require.Equal(t, float64(30), first["total"])
	require.Equal(t, float64(25), first["limit"])
	require.Equal(t, float64(0), first["offset"])
	require.Equal(t, float64(25), first["next_offset"])
	require.Contains(t, first, "timestamp")

	last := listProjectsTool(t, svc, map[string]interface{}{"tenant_id": "tenant-1", "offset": 25})

	ids = pageItemIDs(t, last, "projects")
	require.Len(t, ids, 5)
	require.Equal(t, "proj-4", ids[0])
	require.Equal(t, float64(30), last["total"])
	require.NotContains(t, last, "next_offset", "the last page must carry no next offset")
}

// TestMCPProjectList_SortsOnAllowListedFieldAndRefusesOthers proves sort
// reaches the service and the allow-list refusal reaches this transport as
// a typed error rather than an unordered answer.
func TestMCPProjectList_SortsOnAllowListedFieldAndRefusesOthers(t *testing.T) {
	svc := mcpPagedProjectService(mcpPagedProjects(3), nil)

	sorted := listProjectsTool(t, svc, map[string]interface{}{"tenant_id": "tenant-1", "sort": "name"})
	require.Equal(t, []string{"proj-0", "proj-1", "proj-2"}, pageItemIDs(t, sorted, "projects"))

	reg := newProjectAdminRegistry(t, svc, nil)
	_, err := reg.executeProjectTool(context.Background(), "project_list",
		map[string]interface{}{"tenant_id": "tenant-1", "sort": "runner"})

	require.Error(t, err)
	var unknown *orchestration.UnknownSortFieldError
	require.True(t, errors.As(err, &unknown), "error = %v, want *UnknownSortFieldError", err)
	require.Equal(t, "runner", unknown.Field)
}

// TestMCPProjectList_SearchNarrows proves q reaches the service and narrows
// the total along with the page.
func TestMCPProjectList_SearchNarrows(t *testing.T) {
	var captured *service.ListProjectsRequest
	svc := mcpPagedProjectService(mcpPagedProjects(4), &captured)

	decoded := listProjectsTool(t, svc, map[string]interface{}{"tenant_id": "tenant-1", "q": "service-3", "limit": 10})

	require.Equal(t, []string{"proj-3"}, pageItemIDs(t, decoded, "projects"))
	require.Equal(t, float64(1), decoded["total"])
	require.Equal(t, "tenant-1", captured.TenantID)
	require.Equal(t, "service-3", captured.Page.Query)
	require.Equal(t, 10, captured.Page.Limit)
}

// TestMCPProjectList_DeclaresThePagingArguments proves the schema offers the
// shared paging parameters so a model can page without guessing.
func TestMCPProjectList_DeclaresThePagingArguments(t *testing.T) {
	reg := newProjectAdminRegistry(t, &mockProjectService{}, nil)

	properties := toolSchemaProperties(t, reg, "project_list")
	for _, name := range []string{"tenant_id", "limit", "offset", "sort", "q"} {
		require.Contains(t, properties, name)
	}
}

// =============================================================================
// quicbaas_agent_account_list
// =============================================================================

func listAccountsTool(t *testing.T, svc service.AccountService, args map[string]interface{}) map[string]interface{} {
	t.Helper()

	reg := newAccountRegistry(t, svc)
	result, err := reg.ExecuteTool(context.Background(), "quicbaas_agent_account_list", args)
	require.NoError(t, err)
	return decodedToolResult(t, result)
}

// TestMCPAccountList_DefaultPageAndOffsetWalk proves an argument-free call
// answers with the default page of 25 and the matching total, and that
// offset walks to the last page.
func TestMCPAccountList_DefaultPageAndOffsetWalk(t *testing.T) {
	var captured *service.ListAccountsRequest
	svc := mcpPagedAccountService(mcpPagedAccounts(30), &captured)

	first := listAccountsTool(t, svc, map[string]interface{}{"tenant_id": "tenant-1"})

	require.NotNil(t, captured.Page, "the tool must pass a non-nil page")
	ids := pageItemIDs(t, first, "accounts")
	require.Len(t, ids, 25)
	require.Equal(t, "acct-29", ids[0], "newest first")
	require.Equal(t, float64(30), first["total"])
	require.Equal(t, float64(25), first["limit"])
	require.Equal(t, float64(0), first["offset"])
	require.Equal(t, float64(25), first["next_offset"])
	require.Contains(t, first, "timestamp")

	last := listAccountsTool(t, svc, map[string]interface{}{"tenant_id": "tenant-1", "offset": 25})

	ids = pageItemIDs(t, last, "accounts")
	require.Len(t, ids, 5)
	require.Equal(t, "acct-4", ids[0])
	require.NotContains(t, last, "next_offset", "the last page must carry no next offset")
}

// TestMCPAccountList_SortsOnAllowListedFieldAndRefusesOthers proves sort
// reaches the service and an unknown field is refused with a typed error.
func TestMCPAccountList_SortsOnAllowListedFieldAndRefusesOthers(t *testing.T) {
	svc := mcpPagedAccountService(mcpPagedAccounts(3), nil)

	sorted := listAccountsTool(t, svc, map[string]interface{}{"tenant_id": "tenant-1", "sort": "display_name:desc"})
	require.Equal(t, []string{"acct-2", "acct-1", "acct-0"}, pageItemIDs(t, sorted, "accounts"))

	reg := newAccountRegistry(t, svc)
	_, err := reg.ExecuteTool(context.Background(), "quicbaas_agent_account_list",
		map[string]interface{}{"tenant_id": "tenant-1", "sort": "secret"})

	require.Error(t, err)
	var unknown *orchestration.UnknownSortFieldError
	require.True(t, errors.As(err, &unknown), "error = %v, want *UnknownSortFieldError", err)
	require.Equal(t, "secret", unknown.Field)
}

// TestMCPAccountList_SearchComposesWithTheFilters proves q narrows the page
// and reaches the service beside the provider and project filters.
func TestMCPAccountList_SearchComposesWithTheFilters(t *testing.T) {
	var captured *service.ListAccountsRequest
	svc := mcpPagedAccountService(mcpPagedAccounts(4), &captured)

	decoded := listAccountsTool(t, svc, map[string]interface{}{
		"tenant_id":  "tenant-1",
		"provider":   "claude",
		"project_id": "proj-1",
		"q":          "alice-2",
	})

	require.Equal(t, []string{"acct-2"}, pageItemIDs(t, decoded, "accounts"))
	require.Equal(t, float64(1), decoded["total"])
	require.Equal(t, orchestration.AccountProviderClaude, captured.Provider)
	require.Equal(t, "proj-1", captured.ProjectID)
	require.Equal(t, "alice-2", captured.Page.Query)
}

// TestMCPAccountList_DeclaresThePagingArguments proves the schema offers the
// shared paging parameters.
func TestMCPAccountList_DeclaresThePagingArguments(t *testing.T) {
	reg := newAccountRegistry(t, &mocks.MockAccountService{})

	properties := toolSchemaProperties(t, reg, "quicbaas_agent_account_list")
	for _, name := range []string{"tenant_id", "provider", "project_id", "limit", "offset", "sort", "q"} {
		require.Contains(t, properties, name)
	}
}
