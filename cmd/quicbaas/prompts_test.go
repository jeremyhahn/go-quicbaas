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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	promptSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
)

var errPromptBackend = errors.New("prompt backend refused")

// fakePromptClient records the last request of each verb and answers with
// canned values, or with err when it is set.
type fakePromptClient struct {
	err      error
	calls    []string
	list     *promptSDK.ListRequest
	get      *promptSDK.GetRequest
	addr     *promptSDK.Address
	put      *promptSDK.PutRequest
	rollback *promptSDK.RollbackRequest
	reset    *promptSDK.ResetRequest
}

var promptTestTime = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func promptTestRecord() *promptSDK.Record {
	return &promptSDK.Record{
		TenantID: "tenant-a", ProjectID: "web", Scope: "project", Set: "sdlc", Name: "worker",
		Version: 3, ContentHash: "h3", Content: "Work the issue.\nSTOP:", UpdatedAt: promptTestTime,
	}
}

func (f *fakePromptClient) answer(verb string) error {
	f.calls = append(f.calls, verb)
	return f.err
}

func (f *fakePromptClient) List(_ context.Context, req *promptSDK.ListRequest) (*promptSDK.ListResponse, error) {
	f.list = req
	if err := f.answer("List"); err != nil {
		return nil, err
	}
	return &promptSDK.ListResponse{Set: req.Set, Scope: "tenant", Prompts: []promptSDK.ListEntry{
		{Name: "worker", Overridden: true, Version: 2, Effective: promptSDK.VersionRef{Scope: "tenant", ScopeID: "tenant-a", Version: 2}},
		{Name: "reviewer", Description: "Reviews a PR", Effective: promptSDK.VersionRef{Scope: "default"}},
	}}, nil
}

func (f *fakePromptClient) Get(_ context.Context, req *promptSDK.GetRequest) (*promptSDK.Record, error) {
	f.get = req
	if err := f.answer("Get"); err != nil {
		return nil, err
	}
	return promptTestRecord(), nil
}

func (f *fakePromptClient) Effective(_ context.Context, addr *promptSDK.Address) (*promptSDK.EffectivePrompt, error) {
	f.addr = addr
	if err := f.answer("Effective"); err != nil {
		return nil, err
	}
	return &promptSDK.EffectivePrompt{
		Set: addr.Set, Name: addr.Name, Scope: "project", Content: "Effective body.\n",
		Origin: promptSDK.VersionRef{Scope: "tenant", ScopeID: "tenant-a", Version: 2},
		Chain: []promptSDK.Layer{
			{Scope: "project", ScopeID: "web", Present: true, FollowParent: true},
			{Scope: "tenant", ScopeID: "tenant-a", Present: true, Version: 2, Answered: true},
		},
	}, nil
}

func (f *fakePromptClient) Diff(_ context.Context, addr *promptSDK.Address) (*promptSDK.Diff, error) {
	f.addr = addr
	if err := f.answer("Diff"); err != nil {
		return nil, err
	}
	return &promptSDK.Diff{
		Set: addr.Set, Name: addr.Name, Scope: "tenant",
		Origin:       promptSDK.VersionRef{Scope: "tenant", ScopeID: "tenant-a", Version: 2},
		ParentOrigin: promptSDK.VersionRef{Scope: "default"},
		Lines: []promptSDK.DiffLine{
			{Op: promptSDK.DiffEqual, Text: "Work the issue."},
			{Op: promptSDK.DiffDelete, Text: "old\n"},
			{Op: promptSDK.DiffInsert, Text: "new"},
		},
	}, nil
}

func (f *fakePromptClient) History(_ context.Context, addr *promptSDK.Address) (*promptSDK.History, error) {
	f.addr = addr
	if err := f.answer("History"); err != nil {
		return nil, err
	}
	return &promptSDK.History{Set: addr.Set, Name: addr.Name, Scope: "tenant", Revisions: []promptSDK.RevisionSummary{
		{Version: 2, Author: "alice", Note: "tighten", CreatedAt: promptTestTime},
		{Version: 1, Author: "bob", FollowParent: true},
	}}, nil
}

func (f *fakePromptClient) Put(_ context.Context, req *promptSDK.PutRequest) (*promptSDK.Record, error) {
	f.put = req
	if err := f.answer("Put"); err != nil {
		return nil, err
	}
	return promptTestRecord(), nil
}

func (f *fakePromptClient) Rollback(_ context.Context, req *promptSDK.RollbackRequest) (*promptSDK.Record, error) {
	f.rollback = req
	if err := f.answer("Rollback"); err != nil {
		return nil, err
	}
	return promptTestRecord(), nil
}

func (f *fakePromptClient) Reset(_ context.Context, req *promptSDK.ResetRequest) (*promptSDK.Record, error) {
	f.reset = req
	if err := f.answer("Reset"); err != nil {
		return nil, err
	}
	record := promptTestRecord()
	record.FollowParent, record.Content = true, ""
	return record, nil
}

// withPromptsMock installs fc as the prompt client and resets every flag the
// group shares, restoring all of them when the test ends.
func withPromptsMock(t *testing.T, fc *fakePromptClient) {
	t.Helper()
	origFactory, origTimeout, origTenant, origOpts := promptsClientFactory, globalTimeout, globalTenantID, promptsOpts
	promptsClientFactory = func() (promptSDK.Client, error) { return fc, nil }
	globalTimeout = time.Second
	globalTenantID = "tenant-a"
	promptsOpts = promptsFlags{output: "json"}
	t.Cleanup(func() {
		promptsClientFactory, globalTimeout, globalTenantID, promptsOpts = origFactory, origTimeout, origTenant, origOpts
	})
}

// promptsIO is a command whose streams the test owns.
func promptsIO(stdin string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{}
	var stdout, stderr bytes.Buffer
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	return cmd, &stdout, &stderr
}

// runPrompts executes the group with args, as a user would type them.
func runPrompts(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newPromptsCommand()
	var stdout bytes.Buffer
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

func TestPrompts_EveryVerbSendsItsRequest(t *testing.T) {
	fc := &fakePromptClient{}
	withPromptsMock(t, fc)
	addr := promptSDK.Address{TenantID: "tenant-a", Scope: "project", ProjectID: "web", Set: "sdlc", Name: "worker"}
	scoped := []string{"--scope", "project", "--project", "web"}

	_, err := runPrompts(t, "", append([]string{"list", "sdlc"}, scoped...)...)
	require.NoError(t, err)
	assert.Equal(t, &promptSDK.ListRequest{TenantID: "tenant-a", Scope: "project", ProjectID: "web", Set: "sdlc"}, fc.list)

	_, err = runPrompts(t, "", append([]string{"get", "sdlc", "worker", "--version", "2"}, scoped...)...)
	require.NoError(t, err)
	assert.Equal(t, &promptSDK.GetRequest{Address: addr, Version: 2}, fc.get)

	for _, verb := range []string{"effective", "diff", "history"} {
		fc.addr = nil
		_, err = runPrompts(t, "", append([]string{verb, "sdlc", "worker"}, scoped...)...)
		require.NoError(t, err, verb)
		assert.Equal(t, &addr, fc.addr, verb)
	}

	_, err = runPrompts(t, "New body.\nSTOP:\n", append([]string{"put", "sdlc", "worker", "-f", "-", "--note", "why", "--expected-version", "3"}, scoped...)...)
	require.NoError(t, err)
	assert.Equal(t, &promptSDK.PutRequest{Address: addr, Content: "New body.\nSTOP:\n", Note: "why", ExpectedVersion: 3}, fc.put)

	_, err = runPrompts(t, "", append([]string{"rollback", "sdlc", "worker", "--version", "1", "--note", "back", "--expected-version", "4", "--yes"}, scoped...)...)
	require.NoError(t, err)
	assert.Equal(t, &promptSDK.RollbackRequest{Address: addr, ToVersion: 1, Note: "back", ExpectedVersion: 4}, fc.rollback)

	_, err = runPrompts(t, "", append([]string{"reset", "sdlc", "worker", "--note", "inherit", "--expected-version", "5", "--yes"}, scoped...)...)
	require.NoError(t, err)
	assert.Equal(t, &promptSDK.ResetRequest{Address: addr, Note: "inherit", ExpectedVersion: 5}, fc.reset)

	assert.Equal(t, []string{"List", "Get", "Effective", "Diff", "History", "Put", "Rollback", "Reset"}, fc.calls)
}

func TestPrompts_RefusesBadArgumentsBeforeTheClient(t *testing.T) {
	fc := &fakePromptClient{}
	withPromptsMock(t, fc)
	for _, args := range [][]string{
		{"list"},
		{"get", "sdlc"},
		{"effective", "sdlc", "worker", "extra"},
		{"put", "sdlc", "worker", "--expected-version", "two"},
	} {
		_, err := runPrompts(t, "", args...)
		assert.Error(t, err, "%v", args)
	}
	assert.Empty(t, fc.calls)
}

func TestPrompts_SurfacesClientAndFactoryErrors(t *testing.T) {
	runners := map[string]func(*cobra.Command, []string) error{
		"list": runPromptsList, "get": runPromptsGet, "effective": runPromptsEffective, "diff": runPromptsDiff,
		"history": runPromptsHistory, "rollback": runPromptsRollback, "reset": runPromptsReset,
	}
	fc := &fakePromptClient{err: errPromptBackend}
	withPromptsMock(t, fc)
	promptsOpts.yes = true
	for name, run := range runners {
		cmd, stdout, _ := promptsIO("")
		assert.ErrorIs(t, run(cmd, []string{"sdlc", "worker"}), errPromptBackend, name)
		assert.Empty(t, stdout.String(), name)
	}
	promptsOpts.file = "-"
	cmd, _, _ := promptsIO("body")
	assert.ErrorIs(t, runPromptsPut(cmd, []string{"sdlc", "worker"}), errPromptBackend)

	errNoClient := errors.New("no client")
	promptsClientFactory = func() (promptSDK.Client, error) { return nil, errNoClient }
	for name, run := range runners {
		cmd, _, _ := promptsIO("")
		assert.ErrorIs(t, run(cmd, []string{"sdlc", "worker"}), errNoClient, name)
	}
}

func TestPrompts_PutReadsTheBodyFromAFileOrStdin(t *testing.T) {
	fc := &fakePromptClient{}
	withPromptsMock(t, fc)
	path := filepath.Join(t.TempDir(), "worker.md")
	require.NoError(t, os.WriteFile(path, []byte("From a file.\nSTOP:\n"), 0o600))

	_, err := runPrompts(t, "", "put", "sdlc", "worker", "--file", path, "--note", "n")
	require.NoError(t, err)
	assert.Equal(t, "From a file.\nSTOP:\n", fc.put.Content)

	content, err := readPromptContent("-", strings.NewReader("From stdin.\n"))
	require.NoError(t, err)
	assert.Equal(t, "From stdin.\n", content)
}

func TestPrompts_PutRefusesAMissingBody(t *testing.T) {
	fc := &fakePromptClient{}
	withPromptsMock(t, fc)

	_, err := runPrompts(t, "", "put", "sdlc", "worker", "--note", "n")
	var required *PromptContentRequiredError
	require.ErrorAs(t, err, &required)
	assert.Contains(t, required.Error(), "-f is required")

	_, err = runPrompts(t, "", "put", "sdlc", "worker", "-f", filepath.Join(t.TempDir(), "absent.md"))
	var fileErr *PromptContentFileError
	require.ErrorAs(t, err, &fileErr)
	assert.ErrorIs(t, fileErr, os.ErrNotExist)
	assert.Contains(t, fileErr.Error(), "absent.md")

	_, err = readPromptContent("-", failingReader{})
	require.ErrorAs(t, err, &fileErr)
	assert.Equal(t, "stdin", fileErr.Path)
	assert.Empty(t, fc.calls)
}

// failingReader fails every read.
type failingReader struct{}

var errPromptRead = errors.New("read failed")

func (failingReader) Read([]byte) (int, error) { return 0, errPromptRead }

func TestPrompts_RollbackAndResetAskFirst(t *testing.T) {
	cases := map[string]func(*cobra.Command, []string) error{"rollback": runPromptsRollback, "reset": runPromptsReset}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			fc := &fakePromptClient{}
			withPromptsMock(t, fc)

			cmd, stdout, stderr := promptsIO("n\n")
			require.NoError(t, run(cmd, []string{"sdlc", "worker"}))
			assert.Empty(t, fc.calls, "a declined write must not reach the client")
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "(y/N)")
			assert.Contains(t, stderr.String(), "Cancelled")

			cmd, stdout, _ = promptsIO("")
			require.NoError(t, run(cmd, []string{"sdlc", "worker"}))
			assert.Empty(t, fc.calls, "no answer is a refusal")
			assert.Empty(t, stdout.String())

			cmd, stdout, _ = promptsIO("YES\n")
			require.NoError(t, run(cmd, []string{"sdlc", "worker"}))
			assert.Len(t, fc.calls, 1)
			assert.NotEmpty(t, stdout.String())

			cmd = &cobra.Command{}
			cmd.SetIn(failingReader{})
			cmd.SetErr(&bytes.Buffer{})
			var confirmErr *PromptConfirmError
			require.ErrorAs(t, run(cmd, []string{"sdlc", "worker"}), &confirmErr)
			assert.ErrorIs(t, confirmErr, errPromptRead)
			assert.Contains(t, confirmErr.Error(), "read confirmation")
			assert.Len(t, fc.calls, 1)
		})
	}
}

func TestPrompts_RawPrintsOnlyTheBody(t *testing.T) {
	withPromptsMock(t, &fakePromptClient{})
	out, err := runPrompts(t, "", "get", "sdlc", "worker", "--raw")
	require.NoError(t, err)
	assert.Equal(t, "Work the issue.\nSTOP:\n", out)

	out, err = runPrompts(t, "", "effective", "sdlc", "worker", "--raw")
	require.NoError(t, err)
	assert.Equal(t, "Effective body.\n", out)
}

func TestPrompts_JSONAndYAMLCarryTheAnswer(t *testing.T) {
	withPromptsMock(t, &fakePromptClient{})
	out, err := runPrompts(t, "", "get", "sdlc", "worker", "--output", "json")
	require.NoError(t, err)
	var record promptSDK.Record
	require.NoError(t, json.Unmarshal([]byte(out), &record))
	assert.Equal(t, *promptTestRecord(), record)

	out, err = runPrompts(t, "", "history", "sdlc", "worker", "--output", "yaml")
	require.NoError(t, err)
	var history map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(out), &history))
	assert.Equal(t, "worker", history["name"])
	assert.Len(t, history["revisions"], 2)

	_, err = runPrompts(t, "", "list", "sdlc", "--output", "xml")
	assert.Error(t, err, "an unknown output format must be refused")
}

func TestPrompts_TablesNameWhatMatters(t *testing.T) {
	withPromptsMock(t, &fakePromptClient{})
	table := func(args ...string) string {
		out, err := runPrompts(t, "", append(args, "--output", "table")...)
		require.NoError(t, err, "%v", args)
		return out
	}

	list := table("list", "sdlc")
	assert.Contains(t, list, "NAME")
	assert.Contains(t, list, "tenant tenant-a v2")
	assert.Contains(t, list, "Reviews a PR")

	record := table("get", "sdlc", "worker")
	assert.Contains(t, record, "project web")
	assert.Contains(t, record, "2026-10-01T12:00:00Z")
	assert.True(t, strings.HasSuffix(record, "\nWork the issue.\nSTOP:\n"), record)

	reset := table("reset", "sdlc", "worker", "--yes")
	assert.Contains(t, reset, "Follows parent:  yes")
	assert.NotContains(t, reset, "Work the issue.")

	effective := table("effective", "sdlc", "worker")
	assert.Contains(t, effective, "from tenant tenant-a v2")
	assert.Contains(t, effective, "ANSWERED")
	assert.Contains(t, effective, "Effective body.")

	assert.Equal(t, "--- default\n+++ tenant tenant-a v2\n Work the issue.\n-old\n+new\n", table("diff", "sdlc", "worker"))

	history := table("history", "sdlc", "worker")
	assert.Contains(t, history, "alice")
	assert.Contains(t, history, "tighten")
}

func TestFormatPromptDiff_SameAndUnknownOps(t *testing.T) {
	withPromptsMock(t, &fakePromptClient{})
	promptsOpts.output = "table"
	var buf bytes.Buffer
	require.NoError(t, formatPromptDiff(&buf, &promptSDK.Diff{Same: true}))
	assert.Equal(t, "--- -\n+++ -\n(no difference from the parent scope)\n", buf.String())

	buf.Reset()
	require.NoError(t, formatPromptDiff(&buf, &promptSDK.Diff{Lines: []promptSDK.DiffLine{{Op: "replace", Text: "x"}}}))
	assert.Contains(t, buf.String(), "?x")
}

// TestFormatPrompt_SurfacesAFailedWrite proves a table that cannot be written,
// as to a closed stdout, fails the command rather than reporting success.
func TestFormatPrompt_SurfacesAFailedWrite(t *testing.T) {
	withPromptsMock(t, &fakePromptClient{})
	promptsOpts.output = "table"
	errClosed := errors.New("stdout closed")
	w := failingWriter{err: errClosed}
	diffLines := []promptSDK.DiffLine{{Op: "insert", Text: "x"}}

	for name, format := range map[string]func() error{
		"list":      func() error { return formatPromptList(w, &promptSDK.ListResponse{}) },
		"record":    func() error { return formatPromptRecord(w, &promptSDK.Record{}) },
		"effective": func() error { return formatPromptEffective(w, &promptSDK.EffectivePrompt{}) },
		"diff":      func() error { return formatPromptDiff(w, &promptSDK.Diff{Lines: diffLines}) },
		"history":   func() error { return formatPromptHistory(w, &promptSDK.History{}) },
	} {
		assert.ErrorIs(t, format(), errClosed, name)
	}
}

func TestPromptLabels(t *testing.T) {
	assert.Equal(t, "-", promptRefLabel(promptSDK.VersionRef{}))
	assert.Equal(t, "default", promptRefLabel(promptSDK.VersionRef{Scope: "default"}))
	assert.Equal(t, "project web v4", promptRefLabel(promptSDK.VersionRef{Scope: "project", ScopeID: "web", Version: 4}))
	assert.Equal(t, "-", promptScopeLabel("", "", ""))
	assert.Equal(t, "project web", promptScopeLabel("project", "tenant-a", "web"))
	assert.Equal(t, "-", promptVersionLabel(0))
	assert.Equal(t, "7", promptVersionLabel(7))
	assert.Equal(t, "-", promptTimeLabel(time.Time{}))
	assert.Equal(t, "2026-10-01T12:00:00Z", promptTimeLabel(promptTestTime.In(time.FixedZone("x", 3600))))
}

func TestCreateSDKPromptsClient(t *testing.T) {
	origFactory := sdkClientFactory
	t.Cleanup(func() { sdkClientFactory = origFactory })

	errNoSDK := errors.New("no sdk")
	sdkClientFactory = func() (quicbaas.Client, error) { return nil, errNoSDK }
	_, err := createSDKPromptsClient()
	assert.ErrorIs(t, err, errNoSDK)

	sdkClientFactory = func() (quicbaas.Client, error) { return &mockSDKClient{}, nil }
	client, err := createSDKPromptsClient()
	require.NoError(t, err)
	assert.Nil(t, client, "the mock SDK client mounts no prompt client")
}

func TestPromptsCommand_IsRegisteredWithItsFlags(t *testing.T) {
	cmd := newPromptsCommand()
	for _, flag := range []string{"project", "scope", "output"} {
		assert.NotNil(t, cmd.PersistentFlags().Lookup(flag), flag)
	}
	assert.Nil(t, cmd.PersistentFlags().Lookup("tenant"), "the tenant is the global flag")
	verbFlags := map[string][]string{
		"list": nil, "diff": nil, "history": nil,
		"get":       {"version", "raw"},
		"effective": {"raw"},
		"put":       {"file", "note", "expected-version"},
		"rollback":  {"version", "note", "expected-version", "yes"},
		"reset":     {"note", "expected-version", "yes"},
	}
	for verb, flags := range verbFlags {
		sub, _, err := cmd.Find([]string{verb})
		require.NoError(t, err, verb)
		require.Equal(t, verb, sub.Name())
		for _, flag := range flags {
			assert.NotNil(t, sub.Flags().Lookup(flag), "%s --%s", verb, flag)
		}
	}
	put, _, err := cmd.Find([]string{"put"})
	require.NoError(t, err)
	assert.NotNil(t, put.Flags().ShorthandLookup("f"))
	assert.Len(t, cmd.Commands(), len(verbFlags))
}
