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

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
)

// fakeAgentsClient is an in-memory agents.Client that records the tenant each
// verb was called for.
type fakeAgentsClient struct {
	agents  map[string]*agentsSDK.Agent
	pools   map[string]*agentsSDK.Pool
	usages  []agentsSDK.Usage
	tenants []string
	err     error
}

func newFakeAgentsClient() *fakeAgentsClient {
	return &fakeAgentsClient{
		agents: map[string]*agentsSDK.Agent{"claude-coder": {ID: "claude-coder", Name: "Claude coder", Roles: []string{"coding"}, Backend: "claude", Model: "claude-opus-4-7"}},
		pools:  map[string]*agentsSDK.Pool{"coders": {ID: "coders", Name: "Coders", Agents: []string{"claude-coder", "codex-coder"}}},
		usages: []agentsSDK.Usage{{Kind: "binding", ID: "proj-1", Slot: "worker"}},
	}
}

func (f *fakeAgentsClient) seen(tenantID string) error {
	f.tenants = append(f.tenants, tenantID)
	return f.err
}

func (f *fakeAgentsClient) ListAgents(_ context.Context, tenantID string) ([]*agentsSDK.Agent, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	return []*agentsSDK.Agent{f.agents["claude-coder"]}, nil
}

func (f *fakeAgentsClient) GetAgent(_ context.Context, tenantID, id string) (*agentsSDK.Agent, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	return f.agents[id], nil
}

func (f *fakeAgentsClient) CreateAgent(_ context.Context, tenantID string, agent *agentsSDK.Agent) (*agentsSDK.Agent, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	f.agents[agent.ID] = agent
	return agent, nil
}

func (f *fakeAgentsClient) UpdateAgent(_ context.Context, tenantID, id string, agent *agentsSDK.Agent) (*agentsSDK.Agent, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	agent.ID = id
	f.agents[id] = agent
	return agent, nil
}

func (f *fakeAgentsClient) DeleteAgent(_ context.Context, tenantID, id string) error {
	if err := f.seen(tenantID); err != nil {
		return err
	}
	delete(f.agents, id)
	return nil
}

func (f *fakeAgentsClient) AgentUsedBy(_ context.Context, tenantID, _ string) ([]agentsSDK.Usage, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	return f.usages, nil
}

func (f *fakeAgentsClient) ListPools(_ context.Context, tenantID string) ([]*agentsSDK.Pool, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	return []*agentsSDK.Pool{f.pools["coders"]}, nil
}

func (f *fakeAgentsClient) GetPool(_ context.Context, tenantID, id string) (*agentsSDK.Pool, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	return f.pools[id], nil
}

func (f *fakeAgentsClient) CreatePool(_ context.Context, tenantID string, pool *agentsSDK.Pool) (*agentsSDK.Pool, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	f.pools[pool.ID] = pool
	return pool, nil
}

func (f *fakeAgentsClient) UpdatePool(_ context.Context, tenantID, id string, pool *agentsSDK.Pool) (*agentsSDK.Pool, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	pool.ID = id
	f.pools[id] = pool
	return pool, nil
}

func (f *fakeAgentsClient) DeletePool(_ context.Context, tenantID, id string) error {
	if err := f.seen(tenantID); err != nil {
		return err
	}
	delete(f.pools, id)
	return nil
}

func (f *fakeAgentsClient) PoolUsedBy(_ context.Context, tenantID, _ string) ([]agentsSDK.Usage, error) {
	if err := f.seen(tenantID); err != nil {
		return nil, err
	}
	return nil, nil
}

// useFakeAgents swaps the client factory, tenant and output flags for one
// test.
func useFakeAgents(t *testing.T, client *fakeAgentsClient) {
	t.Helper()
	oldFactory, oldTenant, oldFmt, oldFile := agentsClientFactory, globalTenantID, agentsOutputFmt, agentsFile
	agentsClientFactory = func() (agentsSDK.Client, error) { return client, nil }
	globalTenantID, agentsOutputFmt, agentsFile = "acme", "table", ""
	t.Cleanup(func() {
		agentsClientFactory, globalTenantID, agentsOutputFmt, agentsFile = oldFactory, oldTenant, oldFmt, oldFile
	})
}

func writeDefinition(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "def.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestAgentsCommand_EveryVerbReachesTheClientForTheTenant(t *testing.T) {
	client := newFakeAgentsClient()
	useFakeAgents(t, client)

	out := captureStdoutOutput(func() { require.NoError(t, runAgentsList(nil, nil)) })
	assert.Contains(t, out, "claude-coder")
	assert.Contains(t, out, "claude-opus-4-7")

	out = captureStdoutOutput(func() { require.NoError(t, runAgentsGet(nil, []string{"claude-coder"})) })
	assert.Contains(t, out, "Claude coder")

	agentsFile = writeDefinition(t, "id: codex-reviewer\nname: Codex reviewer\nroles: [review]\nbackend: codex\nmodel: gpt-5.5\n")
	captureStdoutOutput(func() { require.NoError(t, runAgentsCreate(nil, nil)) })
	require.Contains(t, client.agents, "codex-reviewer")
	assert.Equal(t, "gpt-5.5", client.agents["codex-reviewer"].Model)

	captureStdoutOutput(func() { require.NoError(t, runAgentsUpdate(nil, []string{"renamed"})) })
	assert.Contains(t, client.agents, "renamed", "update takes its ID from the argument")

	out = captureStdoutOutput(func() { require.NoError(t, runAgentsUsedBy(nil, []string{"claude-coder"})) })
	assert.Contains(t, out, "proj-1")
	assert.Contains(t, out, "worker")

	out = captureStdoutOutput(func() { require.NoError(t, runAgentsDelete(nil, []string{"renamed"})) })
	assert.Contains(t, out, "renamed deleted")
	assert.NotContains(t, client.agents, "renamed")

	out = captureStdoutOutput(func() { require.NoError(t, runAgentPoolsList(nil, nil)) })
	assert.Contains(t, out, "claude-coder -> codex-coder", "a pool renders in failover order")

	captureStdoutOutput(func() { require.NoError(t, runAgentPoolsGet(nil, []string{"coders"})) })
	agentsFile = writeDefinition(t, `{"id":"reviewers","name":"Reviewers","agents":["codex-reviewer"]}`)
	captureStdoutOutput(func() { require.NoError(t, runAgentPoolsCreate(nil, nil)) })
	assert.Contains(t, client.pools, "reviewers")
	captureStdoutOutput(func() { require.NoError(t, runAgentPoolsUpdate(nil, []string{"reviewers"})) })
	out = captureStdoutOutput(func() { require.NoError(t, runAgentPoolsUsedBy(nil, []string{"reviewers"})) })
	assert.Contains(t, out, "Nothing references it.")
	captureStdoutOutput(func() { require.NoError(t, runAgentPoolsDelete(nil, []string{"reviewers"})) })
	assert.NotContains(t, client.pools, "reviewers")

	for _, tenant := range client.tenants {
		assert.Equal(t, "acme", tenant)
	}
}

func TestAgentsCommand_RefusesAMissingOrUnreadableDefinition(t *testing.T) {
	useFakeAgents(t, newFakeAgentsClient())

	var required *AgentsFileRequiredError
	err := runAgentsCreate(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--file is required")

	agentsFile = filepath.Join(t.TempDir(), "absent.yaml")
	assert.Error(t, runAgentsUpdate(nil, []string{"x"}))
	assert.Error(t, runAgentPoolsCreate(nil, nil))

	agentsFile = writeDefinition(t, "id: [unterminated")
	assert.Error(t, runAgentPoolsUpdate(nil, []string{"x"}))

	_, err = readAgentsFile[agentsSDK.Agent]("create", "")
	assert.ErrorAs(t, err, &required)
	var fileErr *AgentsFileError
	_, err = readAgentsFile[agentsSDK.Agent]("create", agentsFile)
	require.ErrorAs(t, err, &fileErr)
	assert.NotNil(t, errors.Unwrap(fileErr))
	assert.Contains(t, fileErr.Error(), agentsFile)
}

func TestAgentsCommand_ReportsAClientFailure(t *testing.T) {
	client := newFakeAgentsClient()
	client.err = errors.New("agent catalog unavailable")
	useFakeAgents(t, client)
	agentsFile = writeDefinition(t, "id: a\n")

	verbs := map[string]func() error{
		"list":        func() error { return runAgentsList(nil, nil) },
		"get":         func() error { return runAgentsGet(nil, []string{"a"}) },
		"create":      func() error { return runAgentsCreate(nil, nil) },
		"update":      func() error { return runAgentsUpdate(nil, []string{"a"}) },
		"delete":      func() error { return runAgentsDelete(nil, []string{"a"}) },
		"used-by":     func() error { return runAgentsUsedBy(nil, []string{"a"}) },
		"pool list":   func() error { return runAgentPoolsList(nil, nil) },
		"pool get":    func() error { return runAgentPoolsGet(nil, []string{"a"}) },
		"pool create": func() error { return runAgentPoolsCreate(nil, nil) },
		"pool update": func() error { return runAgentPoolsUpdate(nil, []string{"a"}) },
		"pool delete": func() error { return runAgentPoolsDelete(nil, []string{"a"}) },
		"pool usedby": func() error { return runAgentPoolsUsedBy(nil, []string{"a"}) },
	}
	for name, run := range verbs {
		assert.Error(t, run(), name)
	}

	agentsClientFactory = func() (agentsSDK.Client, error) { return nil, errors.New("no transport") }
	assert.Error(t, runAgentsList(nil, nil))
}

func TestAgentsOutput_StructuredFormats(t *testing.T) {
	agent := &agentsSDK.Agent{ID: "a", Name: "A", Backend: "codex", Model: "gpt-5.5", ContextTokens: 1000}
	var js bytes.Buffer
	require.NoError(t, formatAgent(agent, "json", &js))
	assert.Contains(t, js.String(), `"model": "gpt-5.5"`)

	var yml bytes.Buffer
	require.NoError(t, formatAgentPools([]*agentsSDK.Pool{{ID: "p", Agents: []string{"a"}}}, "yaml", &yml))
	assert.Contains(t, yml.String(), "id: p")

	var table bytes.Buffer
	require.NoError(t, formatAgent(agent, "table", &table))
	assert.Contains(t, table.String(), "1000")
	assert.Contains(t, table.String(), "Max concurrent:")

	var bad bytes.Buffer
	assert.Error(t, formatAgentList(nil, "toml", &bad))
	assert.Error(t, formatAgentUsages(nil, "toml", &bad))
	assert.Equal(t, "-", intOrDash(0))
	assert.Equal(t, "7", intOrDash(7))
}

func TestAgentsCommand_HasEveryVerb(t *testing.T) {
	cmd := newAgentsCommand()
	var verbs []string
	for _, sub := range cmd.Commands() {
		verbs = append(verbs, sub.Name())
	}
	for _, want := range []string{"list", "get", "create", "update", "delete", "used-by", "pools"} {
		assert.Contains(t, verbs, want)
	}
	pools, _, err := cmd.Find([]string{"pools", "used-by"})
	require.NoError(t, err)
	assert.Equal(t, "used-by", pools.Name())
}
