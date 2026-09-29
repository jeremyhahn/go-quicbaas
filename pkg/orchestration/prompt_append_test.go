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

package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestComposePrompt(t *testing.T) {
	cases := map[string]struct {
		prompt, appended, want string
	}{
		"both":               {"header", "dynamic", "header" + PromptAppendSeparator + "dynamic"},
		"no prompt":          {"", "dynamic", "dynamic"},
		"nothing appended":   {"header", "", "header"},
		"blank appended":     {"header", " \n\t", "header"},
		"neither":            {"", "", ""},
		"text kept verbatim": {"  header\n", "dynamic\n", "  header\n" + PromptAppendSeparator + "dynamic\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ComposePrompt(tc.prompt, tc.appended); got != tc.want {
				t.Fatalf("ComposePrompt(%q, %q) = %q, want %q", tc.prompt, tc.appended, got, tc.want)
			}
		})
	}
}

func TestComposePrompt_KeepsOrderAndDropsBlanks(t *testing.T) {
	got := ComposePrompt("slot", "", "  ", "job")
	if want := "slot" + PromptAppendSeparator + "job"; got != want {
		t.Fatalf("ComposePrompt = %q, want %q", got, want)
	}
	if got := ComposePrompt(); got != "" {
		t.Fatalf("ComposePrompt() = %q, want empty", got)
	}
}

func TestValidatePromptAppend(t *testing.T) {
	if err := ValidatePromptAppend("prompt_append", strings.Repeat("a", MaxPromptAppendBytes)); err != nil {
		t.Fatalf("material at the limit refused: %v", err)
	}
	if err := ValidatePromptAppend("prompt_append", ""); err != nil {
		t.Fatalf("no material refused: %v", err)
	}
	err := ValidatePromptAppend("task.prompt_append", strings.Repeat("a", MaxPromptAppendBytes+1))
	var tooLarge *PromptAppendTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want PromptAppendTooLargeError", err)
	}
	if tooLarge.Field != "task.prompt_append" || tooLarge.Size != MaxPromptAppendBytes+1 || tooLarge.Limit != MaxPromptAppendBytes {
		t.Fatalf("refusal = %+v", tooLarge)
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a refusal must read as bad input on every transport: %v", err)
	}
	if !strings.Contains(err.Error(), "task.prompt_append") {
		t.Fatalf("the refusal names no field: %v", err)
	}
}

func TestSpawnSession_AppendsRunTimeMaterialAfterTheAgentPrompt(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	if _, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{AgentID: "first", PromptAppend: "issue #7"}); err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	got := f.primary.requests()
	want := "you are first" + PromptAppendSeparator + "issue #7"
	if len(got) != 1 || got[0].SystemPrompt != want {
		t.Fatalf("the backend saw %+v, want system prompt %q", got, want)
	}
}

func TestSpawnSession_RefusesOversizedPromptAppend(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	_, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		AgentID:      "first",
		PromptAppend: strings.Repeat("a", MaxPromptAppendBytes+1),
	})
	var tooLarge *PromptAppendTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want PromptAppendTooLargeError", err)
	}
	if len(f.primary.requests()) != 0 {
		t.Fatal("an oversized append must be refused before anything is spawned")
	}
}

func TestDurableSessionRecord_KeepsThePromptAndTheAppendApart(t *testing.T) {
	record := newDurableSessionRecord(
		&SpawnSessionRequest{Backend: "claude", SystemPrompt: "header", PromptAppend: "dynamic"},
		"tenant-a", "/work", "model-x", SandboxModeDefault, TrustLevelNone, "", true)
	if record.SystemPrompt != "header" || record.PromptAppend != "dynamic" {
		t.Fatalf("record = {SystemPrompt:%q PromptAppend:%q}", record.SystemPrompt, record.PromptAppend)
	}
	want := "header" + PromptAppendSeparator + "dynamic"
	if got := spawnRequestFromRecord(record).SystemPrompt; got != want {
		t.Fatalf("a resume spawns with %q, want %q", got, want)
	}
	if got := spawnRequestFromRecord(&SessionRecord{SystemPrompt: "header"}).SystemPrompt; got != "header" {
		t.Fatalf("a record with nothing appended resumes with %q, want the prompt alone", got)
	}
}

func TestJobRunSpawn_AppendsTheJobsMaterialAfterTheSlots(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	config := newTestConfig()
	config.PromptAppend = "job material"
	run := newJobRun(JobRunConfig{JobID: "job-1", Config: config, Agents: f.svc, Logger: &controlLoopMockLogger{}})
	if _, err := run.Spawn(context.Background(), &SpawnSessionRequest{AgentID: "first", PromptAppend: "slot material"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	got := f.primary.requests()
	want := ComposePrompt("you are first", "slot material", "job material")
	if len(got) != 1 || got[0].SystemPrompt != want {
		t.Fatalf("the backend saw %+v, want system prompt %q", got, want)
	}
	var audited bool
	for _, event := range run.auditEventsCopy() {
		audited = audited || event.Type == "prompt:append"
	}
	if !audited {
		t.Fatalf("the append is not on the job's trail: %+v", run.auditEventsCopy())
	}
}

func TestJobRunSpawn_RefusesAJobAndSlotAppendOverTheBound(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	config := newTestConfig()
	config.PromptAppend = strings.Repeat("j", MaxPromptAppendBytes)
	run := newJobRun(JobRunConfig{JobID: "job-1", Config: config, Agents: f.svc, Logger: &controlLoopMockLogger{}})
	_, err := run.Spawn(context.Background(), &SpawnSessionRequest{AgentID: "first", PromptAppend: "slot"})
	var tooLarge *PromptAppendTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want PromptAppendTooLargeError: the bound covers everything appended", err)
	}
}

func TestWorkflowDefinition_RefusesAnOversizedRoleAppend(t *testing.T) {
	wd := newValidWorkflowDef("wf")
	if err := wd.Validate(); err != nil {
		t.Fatalf("the base workflow is refused: %v", err)
	}
	wd.Agents[0].PromptAppend = strings.Repeat("a", MaxPromptAppendBytes+1)
	var tooLarge *PromptAppendTooLargeError
	if err := wd.Validate(); !errors.As(err, &tooLarge) || tooLarge.Field != "agents[0].prompt_append" {
		t.Fatalf("Validate = %v, want PromptAppendTooLargeError on agents[0].prompt_append", err)
	}
}

func TestCreateTask_CarriesItsPromptAppend(t *testing.T) {
	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svc.(*agentServiceImpl).SetTaskStore(store)
	task, err := svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1", Subject: "fix bug", PromptAppend: "touch only pkg/foo",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.PromptAppend != "touch only pkg/foo" {
		t.Fatalf("task.PromptAppend = %q", task.PromptAppend)
	}
}

func TestCreateTask_RefusesAnOversizedPromptAppend(t *testing.T) {
	store := &mockTaskStore{}
	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svc.(*agentServiceImpl).SetTaskStore(store)
	_, err = svc.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID: "tenant-1", Subject: "fix bug", PromptAppend: strings.Repeat("a", MaxPromptAppendBytes+1),
	})
	var tooLarge *PromptAppendTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want PromptAppendTooLargeError", err)
	}
	if len(store.createCalls) != 0 {
		t.Fatal("a refused task must not be stored")
	}
}

func TestStartJob_RecordsAndBoundsItsPromptAppend(t *testing.T) {
	svc := newMockBackendService(t)
	defer cancelAllJobs(svc)
	_, err := svc.StartJob(context.Background(), &StartJobRequest{
		TenantID: "tenant-1", Description: "work", routedBackend: "mock", MaxIterations: 1,
		PromptAppend: strings.Repeat("a", MaxPromptAppendBytes+1),
	})
	var tooLarge *PromptAppendTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want PromptAppendTooLargeError", err)
	}
	if n := countRunningJobs(svc); n != 0 {
		t.Fatalf("a refused request starts nothing, %d jobs were launched", n)
	}

	resp, err := svc.StartJob(context.Background(), &StartJobRequest{
		TenantID: "tenant-1", Description: "work", routedBackend: "mock", MaxIterations: 1,
		PromptAppend: "dynamic",
	})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	status, err := svc.GetJobStatus(context.Background(), "tenant-1", resp.JobID)
	if err != nil {
		t.Fatalf("GetJobStatus: %v", err)
	}
	if status.Config == nil || status.Config.PromptAppend != "dynamic" {
		t.Fatalf("the job's config does not record its append: %+v", status.Config)
	}
}
