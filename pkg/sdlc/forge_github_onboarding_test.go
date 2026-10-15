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

package sdlc

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

const (
	githubLabelsPath = "/repos/acme/widgets/labels"
	githubHooksPath  = "/repos/acme/widgets/hooks"
	githubHookURL    = "https://factory.example/hook/p1"
)

// githubHookAt is one webhook as GitHub lists it.
func githubHookAt(id int64, hookURL string, active bool, events ...string) map[string]any {
	return map[string]any{"id": id, "active": active, "events": events, "config": map[string]any{"url": hookURL}}
}

func TestGitHubLabels_NormalizesTheColor(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET "+githubLabelsPath, http.StatusOK, []map[string]string{
		{"name": "task:ready", "color": "#0E8A16", "description": "ready to start"},
	})

	labels, err := backend.Labels(context.Background(), githubRepo)
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	want := []ForgeLabel{{Name: "task:ready", Color: "0e8a16", Description: "ready to start"}}
	if !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels = %+v, want %+v", labels, want)
	}
}

func TestGitHubLabels_RefusalIsReturned(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET "+githubLabelsPath, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})

	if _, err := backend.Labels(context.Background(), githubRepo); err == nil {
		t.Fatal("Labels succeeded against a refusal")
	}
}

func TestGitHubEnsureLabels_CreatesOnlyTheDifference(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.sequence("GET "+githubLabelsPath,
		forgeJSON(http.StatusOK, []map[string]string{{"name": "task:ready", "color": "0e8a16"}}),
		forgeJSON(http.StatusOK, []map[string]string{
			{"name": "task:ready", "color": "0e8a16"},
			{"name": "factory:merged", "color": "5319e7"},
		}),
	)
	stub.json("POST "+githubLabelsPath, http.StatusCreated, map[string]string{"name": "factory:merged"})

	err := backend.EnsureLabels(context.Background(), githubRepo, []LabelSpec{
		{Name: "task:ready"},
		{Name: "factory:merged", Color: "5319e7", Description: "landed"},
	})
	if err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	created := stub.requests("POST " + githubLabelsPath)
	if len(created) != 1 {
		t.Fatalf("created %d labels, want 1", len(created))
	}
	body := decodeBody(t, created[0])
	if body["name"] != "factory:merged" || body["color"] != "5319e7" || body["description"] != "landed" {
		t.Fatalf("create body = %v", body)
	}
}

func TestGitHubEnsureLabels_NothingMissingWritesNothing(t *testing.T) {
	backend, stub := newGitHubStub(t)
	githubLabelsDefined(stub, "task:ready")

	if err := backend.EnsureLabels(context.Background(), githubRepo, nil); err != nil {
		t.Fatalf("EnsureLabels(nil): %v", err)
	}
	if err := backend.EnsureLabels(context.Background(), githubRepo, []LabelSpec{{Name: "Task:Ready"}}); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Fatalf("writes = %+v, want none", writes)
	}
}

func TestGitHubEnsureLabels_Refusals(t *testing.T) {
	t.Run("an unnamed spec", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		err := backend.EnsureLabels(context.Background(), githubRepo, []LabelSpec{{Color: "0e8a16"}})
		if err == nil {
			t.Fatal("EnsureLabels accepted a label with no name")
		}
		if calls := stub.requests("GET " + githubLabelsPath); len(calls) != 0 {
			t.Fatalf("read labels %d times before validating", len(calls))
		}
	})

	t.Run("the first read fails", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET "+githubLabelsPath, http.StatusForbidden, map[string]string{"message": "forbidden"})
		if err := backend.EnsureLabels(context.Background(), githubRepo, []LabelSpec{{Name: "a"}}); err == nil {
			t.Fatal("EnsureLabels succeeded without reading the labels")
		}
	})

	t.Run("the create is refused", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		githubLabelsDefined(stub)
		stub.json("POST "+githubLabelsPath, http.StatusUnprocessableEntity, map[string]string{"message": "invalid color"})
		if err := backend.EnsureLabels(context.Background(), githubRepo, []LabelSpec{{Name: "a"}}); err == nil {
			t.Fatal("EnsureLabels succeeded through a refused create")
		}
	})

	t.Run("the read-back fails", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.sequence("GET "+githubLabelsPath,
			forgeJSON(http.StatusOK, []map[string]string{}),
			forgeJSON(http.StatusForbidden, map[string]string{"message": "forbidden"}),
		)
		stub.json("POST "+githubLabelsPath, http.StatusCreated, map[string]string{"name": "a"})
		if err := backend.EnsureLabels(context.Background(), githubRepo, []LabelSpec{{Name: "a"}}); err == nil {
			t.Fatal("EnsureLabels succeeded without its read-back")
		}
	})

	t.Run("the label never appears", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		githubLabelsDefined(stub, "other")
		stub.json("POST "+githubLabelsPath, http.StatusCreated, map[string]string{"name": "a"})
		err := backend.EnsureLabels(context.Background(), githubRepo, []LabelSpec{{Name: "a"}})
		var notApplied *ForgeWriteNotAppliedError
		if !errors.As(err, &notApplied) {
			t.Fatalf("err = %v, want ForgeWriteNotAppliedError", err)
		}
	})
}

func TestGitHubWebhooks_MapsEveryHook(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET "+githubHooksPath, http.StatusOK, []map[string]any{
		githubHookAt(7, githubHookURL, true, "issues", "pull_request"),
	})

	hooks, err := backend.Webhooks(context.Background(), githubRepo)
	if err != nil {
		t.Fatalf("Webhooks: %v", err)
	}
	want := []ForgeWebhook{{ID: "7", URL: githubHookURL, Active: true, Events: []string{"issues", "pull_request"}}}
	if !reflect.DeepEqual(hooks, want) {
		t.Fatalf("hooks = %+v, want %+v", hooks, want)
	}
}

func TestGitHubWebhooks_RefusalIsReturned(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET "+githubHooksPath, http.StatusForbidden, map[string]string{"message": "admin required"})

	if _, err := backend.Webhooks(context.Background(), githubRepo); err == nil {
		t.Fatal("Webhooks succeeded against a refusal")
	}
}

func TestGitHubDeleteWebhook_DeletesEveryMatch(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.sequence("GET "+githubHooksPath,
		forgeJSON(http.StatusOK, []map[string]any{
			githubHookAt(7, githubHookURL, true, "issues"),
			githubHookAt(8, "https://elsewhere.example/hook", true, "issues"),
			githubHookAt(9, githubHookURL, false, "issues"),
		}),
		forgeJSON(http.StatusOK, []map[string]any{githubHookAt(8, "https://elsewhere.example/hook", true, "issues")}),
	)
	stub.json("DELETE "+githubHooksPath+"/7", http.StatusNoContent, nil)
	// Already gone by the time it is deleted: the desired state either way.
	stub.json("DELETE "+githubHooksPath+"/9", http.StatusNotFound, map[string]string{"message": "Not Found"})

	if err := backend.DeleteWebhook(context.Background(), githubRepo, githubHookURL); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if writes := stub.writes(); len(writes) != 2 {
		t.Fatalf("writes = %+v, want the two matching deletes", writes)
	}
}

func TestGitHubDeleteWebhook_NothingToDelete(t *testing.T) {
	t.Run("the repository is gone", func(t *testing.T) {
		backend, _ := newGitHubStub(t)
		if err := backend.DeleteWebhook(context.Background(), githubRepo, githubHookURL); err != nil {
			t.Fatalf("DeleteWebhook: %v", err)
		}
	})

	t.Run("no hook delivers there", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET "+githubHooksPath, http.StatusOK, []map[string]any{githubHookAt(8, "https://elsewhere.example/hook", true)})
		if err := backend.DeleteWebhook(context.Background(), githubRepo, githubHookURL); err != nil {
			t.Fatalf("DeleteWebhook: %v", err)
		}
		if writes := stub.writes(); len(writes) != 0 {
			t.Fatalf("writes = %+v, want none", writes)
		}
	})
}

func TestGitHubDeleteWebhook_Refusals(t *testing.T) {
	t.Run("no url", func(t *testing.T) {
		backend, _ := newGitHubStub(t)
		var invalid *InvalidInputError
		if err := backend.DeleteWebhook(context.Background(), githubRepo, "  "); !errors.As(err, &invalid) {
			t.Fatalf("err = %v, want InvalidInputError", err)
		}
	})

	t.Run("the listing is refused", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET "+githubHooksPath, http.StatusForbidden, map[string]string{"message": "admin required"})
		if err := backend.DeleteWebhook(context.Background(), githubRepo, githubHookURL); err == nil {
			t.Fatal("DeleteWebhook succeeded without listing")
		}
	})

	t.Run("the delete is refused", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET "+githubHooksPath, http.StatusOK, []map[string]any{githubHookAt(7, githubHookURL, true)})
		stub.json("DELETE "+githubHooksPath+"/7", http.StatusForbidden, map[string]string{"message": "admin required"})
		if err := backend.DeleteWebhook(context.Background(), githubRepo, githubHookURL); err == nil {
			t.Fatal("DeleteWebhook succeeded through a refused delete")
		}
	})

	t.Run("the read-back fails", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.sequence("GET "+githubHooksPath,
			forgeJSON(http.StatusOK, []map[string]any{githubHookAt(7, githubHookURL, true)}),
			forgeJSON(http.StatusForbidden, map[string]string{"message": "admin required"}),
		)
		stub.json("DELETE "+githubHooksPath+"/7", http.StatusNoContent, nil)
		if err := backend.DeleteWebhook(context.Background(), githubRepo, githubHookURL); err == nil {
			t.Fatal("DeleteWebhook succeeded without its read-back")
		}
	})

	t.Run("the hook survives", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET "+githubHooksPath, http.StatusOK, []map[string]any{githubHookAt(7, githubHookURL, true)})
		stub.json("DELETE "+githubHooksPath+"/7", http.StatusNoContent, nil)
		var notApplied *ForgeWriteNotAppliedError
		if err := backend.DeleteWebhook(context.Background(), githubRepo, githubHookURL); !errors.As(err, &notApplied) {
			t.Fatalf("err = %v, want ForgeWriteNotAppliedError", err)
		}
	})
}

func TestGitHubRepoFiles_ListsADirectory(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/contents/docs/adr", http.StatusOK, []map[string]string{
		{"name": "0001.md", "path": "docs/adr/0001.md", "type": "file"},
		{"name": "drafts", "path": "docs/adr/drafts", "type": "dir"},
	})

	files, err := backend.RepoFiles(context.Background(), githubRepo, " main ", "docs/adr")
	if err != nil {
		t.Fatalf("RepoFiles: %v", err)
	}
	want := []ForgeFile{
		{Path: "docs/adr/0001.md", Name: "0001.md"},
		{Path: "docs/adr/drafts", Name: "drafts", IsDir: true},
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("files = %+v, want %+v", files, want)
	}
	if query := stub.requests("GET /repos/acme/widgets/contents/docs/adr")[0].Query; query != "ref=main" {
		t.Fatalf("query = %q, want ref=main", query)
	}
}

func TestGitHubRepoFiles_SingleFileAndAbsentPaths(t *testing.T) {
	t.Run("a file is one entry", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET /repos/acme/widgets/contents/README.md", http.StatusOK,
			map[string]string{"name": "README.md", "path": "README.md", "type": "file"})
		files, err := backend.RepoFiles(context.Background(), githubRepo, "", "README.md")
		if err != nil || len(files) != 1 || files[0].Path != "README.md" || files[0].IsDir {
			t.Fatalf("files = %+v, %v", files, err)
		}
	})

	t.Run("an absent path is empty", func(t *testing.T) {
		backend, _ := newGitHubStub(t)
		files, err := backend.RepoFiles(context.Background(), githubRepo, "main", "missing")
		if err != nil || files == nil || len(files) != 0 {
			t.Fatalf("files = %#v, %v", files, err)
		}
	})

	t.Run("an empty body is empty", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.handle("GET /repos/acme/widgets/contents", forgeRaw(http.StatusOK, ""))
		files, err := backend.RepoFiles(context.Background(), githubRepo, "", "")
		if err != nil || files == nil || len(files) != 0 {
			t.Fatalf("files = %#v, %v", files, err)
		}
	})
}

func TestGitHubRepoFiles_Refusals(t *testing.T) {
	t.Run("the listing is refused", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET /repos/acme/widgets/contents", http.StatusForbidden, map[string]string{"message": "forbidden"})
		if _, err := backend.RepoFiles(context.Background(), githubRepo, "", ""); err == nil {
			t.Fatal("RepoFiles succeeded against a refusal")
		}
	})

	t.Run("the body is neither shape", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.handle("GET /repos/acme/widgets/contents", forgeRaw(http.StatusOK, `"just a string"`))
		if _, err := backend.RepoFiles(context.Background(), githubRepo, "", ""); err == nil {
			t.Fatal("RepoFiles accepted a body that is neither a listing nor a file")
		}
	})
}

func TestGitHubBranchProtection_MergesBothCheckLists(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/branches/main/protection", http.StatusOK, map[string]any{
		"required_status_checks": map[string]any{
			"contexts": []string{"ci/build", "ci/test"},
			"checks":   []map[string]any{{"context": "ci/test", "app_id": 15368}, {"context": "lint"}, {"context": ""}},
		},
	})

	protection, err := backend.BranchProtection(context.Background(), githubRepo, "main")
	if err != nil {
		t.Fatalf("BranchProtection: %v", err)
	}
	want := &ForgeBranchProtection{Branch: "main", RequireStatusChecks: true, RequiredContexts: []string{"ci/build", "ci/test", "lint"}}
	if !reflect.DeepEqual(protection, want) {
		t.Fatalf("protection = %+v, want %+v", protection, want)
	}
}

func TestGitHubBranchProtection_WithoutRequiredChecks(t *testing.T) {
	t.Run("protected without checks", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET /repos/acme/widgets/branches/main/protection", http.StatusOK, map[string]any{"enforce_admins": map[string]bool{"enabled": true}})
		protection, err := backend.BranchProtection(context.Background(), githubRepo, "main")
		if err != nil || protection == nil || protection.RequireStatusChecks {
			t.Fatalf("protection = %+v, %v", protection, err)
		}
	})

	t.Run("unprotected", func(t *testing.T) {
		backend, _ := newGitHubStub(t)
		protection, err := backend.BranchProtection(context.Background(), githubRepo, "main")
		if err != nil || protection != nil {
			t.Fatalf("protection = %+v, %v; want nil, nil", protection, err)
		}
	})
}

func TestGitHubBranchProtection_Refusals(t *testing.T) {
	t.Run("no branch", func(t *testing.T) {
		backend, _ := newGitHubStub(t)
		var invalid *InvalidInputError
		if _, err := backend.BranchProtection(context.Background(), githubRepo, " "); !errors.As(err, &invalid) {
			t.Fatalf("err = %v, want InvalidInputError", err)
		}
	})

	t.Run("the read is refused", func(t *testing.T) {
		backend, stub := newGitHubStub(t)
		stub.json("GET /repos/acme/widgets/branches/main/protection", http.StatusForbidden, map[string]string{"message": "admin required"})
		if _, err := backend.BranchProtection(context.Background(), githubRepo, "main"); err == nil {
			t.Fatal("BranchProtection succeeded against a refusal")
		}
	})
}
