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
	"slices"
	"testing"
)

const (
	glLabels = glProject + "/labels"
	glTree   = glProject + "/repository/tree"
)

func TestGitLabBackend_Labels_ReportsBareLowercaseHex(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glLabels, http.StatusOK, "labels.json")

	labels, err := backend.Labels(context.Background(), gitlabRepo)
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	want := []ForgeLabel{
		{Name: "bug", Color: "d73a4a", Description: "Something is broken"},
		{Name: "Factory:Queued", Color: "0e8a16", Description: "Waiting for a lane"},
	}
	if !slices.Equal(labels, want) {
		t.Fatalf("labels = %+v, want %+v", labels, want)
	}
}

func TestGitLabBackend_Labels_Refusals(t *testing.T) {
	for name, status := range map[string]int{"unauthorized": http.StatusUnauthorized, "server error": http.StatusInternalServerError} {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			stub.json("GET "+glLabels, status, map[string]any{"message": "refused"})
			if _, err := backend.Labels(context.Background(), gitlabRepo); !errors.Is(err, ErrBackend) {
				t.Fatalf("err = %v, want ErrBackend", err)
			}
		})
	}
	t.Run("absent project", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glLabels, http.StatusNotFound, map[string]any{"message": "404 Project Not Found"})
		if _, err := backend.Labels(context.Background(), gitlabRepo); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestGitLabBackend_EnsureLabels_CreatesOnlyTheMissing(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.sequence("GET "+glLabels,
		forgeJSON(http.StatusOK, []any{map[string]any{"name": "bug", "color": "#D73A4A"}}),
		forgeJSON(http.StatusOK, []any{
			map[string]any{"name": "bug", "color": "#D73A4A"},
			map[string]any{"name": "Factory:Queued", "color": "#0E8A16"},
		}))
	stub.json("POST "+glLabels, http.StatusCreated, map[string]any{"id": 1})

	specs := []LabelSpec{{Name: "bug", Color: "ffffff"}, {Name: "Factory:Queued", Color: "0e8a16", Description: "Waiting"}}
	if err := backend.EnsureLabels(context.Background(), gitlabRepo, specs); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	calls := stub.requests("POST " + glLabels)
	if len(calls) != 1 {
		t.Fatalf("labels created %d times, want only the missing one", len(calls))
	}
	if body := decodeBody(t, calls[0]); body["name"] != "Factory:Queued" || body["color"] != "#0e8a16" || body["description"] != "Waiting" {
		t.Fatalf("label body = %+v", body)
	}
}

func TestGitLabBackend_EnsureLabels_NothingToDo(t *testing.T) {
	t.Run("no specs", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		if err := backend.EnsureLabels(context.Background(), gitlabRepo, nil); err != nil {
			t.Fatalf("EnsureLabels: %v", err)
		}
		if calls := stub.requests("GET " + glLabels); len(calls) != 0 {
			t.Fatal("an empty spec list reached GitLab")
		}
	})
	t.Run("all defined", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.fixture("GET "+glLabels, http.StatusOK, "labels.json")
		if err := backend.EnsureLabels(context.Background(), gitlabRepo, []LabelSpec{{Name: "bug"}}); err != nil {
			t.Fatalf("EnsureLabels: %v", err)
		}
		if writes := stub.writes(); len(writes) != 0 {
			t.Fatalf("a defined label was written: %d writes", len(writes))
		}
	})
}

func TestGitLabBackend_EnsureLabels_Refusals(t *testing.T) {
	specs := []LabelSpec{{Name: "Factory:Queued", Color: "0e8a16"}}

	t.Run("unnamed spec", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		if err := backend.EnsureLabels(context.Background(), gitlabRepo, []LabelSpec{{Name: " "}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("err = %v, want ErrInvalidInput", err)
		}
		if calls := stub.requests("GET " + glLabels); len(calls) != 0 {
			t.Fatal("an invalid spec reached GitLab")
		}
	})
	t.Run("listing refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glLabels, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
		if err := backend.EnsureLabels(context.Background(), gitlabRepo, specs); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
	t.Run("create refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glLabels, http.StatusOK, []any{})
		stub.json("POST "+glLabels, http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		if err := backend.EnsureLabels(context.Background(), gitlabRepo, specs); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
	t.Run("read-back refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.sequence("GET "+glLabels,
			forgeJSON(http.StatusOK, []any{}),
			forgeJSON(http.StatusInternalServerError, map[string]any{"message": "500"}))
		stub.json("POST "+glLabels, http.StatusCreated, map[string]any{"id": 1})
		if err := backend.EnsureLabels(context.Background(), gitlabRepo, specs); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
	t.Run("read-back still missing", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glLabels, http.StatusOK, []any{})
		stub.json("POST "+glLabels, http.StatusCreated, map[string]any{"id": 1})
		if err := backend.EnsureLabels(context.Background(), gitlabRepo, specs); !errors.Is(err, ErrForgeWriteNotApplied) {
			t.Fatalf("err = %v, want ErrForgeWriteNotApplied", err)
		}
	})
}

func TestGitLabBackend_Webhooks_ReportsEventsNotTokens(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glHooks, http.StatusOK, []any{
		gitlabHookAnswer(3, gitlabHookURL, "executable", "merge_requests_events"),
		gitlabHookAnswer(4, "https://elsewhere.example/hook", "disabled", "pipeline_events", "job_events"),
	})

	hooks, err := backend.Webhooks(context.Background(), gitlabRepo)
	if err != nil {
		t.Fatalf("Webhooks: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("hooks = %+v, want 2", hooks)
	}
	first, second := hooks[0], hooks[1]
	if first.ID != "3" || first.URL != gitlabHookURL || !first.Active ||
		!slices.Equal(first.Events, []string{"pull_request", "pull_request_review"}) {
		t.Fatalf("first hook = %+v", first)
	}
	if second.ID != "4" || second.Active || !slices.Equal(second.Events, []string{"status"}) {
		t.Fatalf("second hook = %+v", second)
	}
}

func TestGitLabBackend_Webhooks_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glHooks, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
	if _, err := backend.Webhooks(context.Background(), gitlabRepo); !errors.Is(err, ErrBackend) {
		t.Fatalf("err = %v, want ErrBackend", err)
	}
}

func TestGitLabBackend_DeleteWebhook_RemovesEveryDeliveryToTheURL(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.sequence("GET "+glHooks,
		forgeJSON(http.StatusOK, []any{
			gitlabHookAnswer(2, "https://elsewhere.example/hook", "executable"),
			gitlabHookAnswer(3, gitlabHookURL, "executable"),
			gitlabHookAnswer(4, gitlabHookURL, "executable"),
		}),
		forgeJSON(http.StatusOK, []any{gitlabHookAnswer(2, "https://elsewhere.example/hook", "executable")}))
	stub.json("DELETE "+glHooks+"/3", http.StatusNoContent, nil)
	// A hook removed concurrently is already gone, which is what was asked.
	stub.json("DELETE "+glHooks+"/4", http.StatusNotFound, map[string]any{"message": "404 Not Found"})

	if err := backend.DeleteWebhook(context.Background(), gitlabRepo, gitlabHookURL); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if calls := stub.requests("DELETE " + glHooks + "/2"); len(calls) != 0 {
		t.Fatal("a hook to another URL was deleted")
	}
}

func TestGitLabBackend_DeleteWebhook_NothingToRemove(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"no matching hook": forgeJSON(http.StatusOK, []any{gitlabHookAnswer(2, "https://elsewhere.example/hook", "executable")}),
		"absent project":   forgeJSON(http.StatusNotFound, map[string]any{"message": "404 Project Not Found"}),
	}
	for name, listing := range cases {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			stub.handle("GET "+glHooks, listing)
			if err := backend.DeleteWebhook(context.Background(), gitlabRepo, gitlabHookURL); err != nil {
				t.Fatalf("DeleteWebhook: %v", err)
			}
			if writes := stub.writes(); len(writes) != 0 {
				t.Fatalf("%d writes with nothing to delete", len(writes))
			}
		})
	}
}

func TestGitLabBackend_DeleteWebhook_Refusals(t *testing.T) {
	present := forgeJSON(http.StatusOK, []any{gitlabHookAnswer(3, gitlabHookURL, "executable")})

	t.Run("no url", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		if err := backend.DeleteWebhook(context.Background(), gitlabRepo, " "); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("err = %v, want ErrInvalidInput", err)
		}
		if calls := stub.requests("GET " + glHooks); len(calls) != 0 {
			t.Fatal("a delete naming no URL reached GitLab")
		}
	})
	t.Run("listing refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glHooks, http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		if err := backend.DeleteWebhook(context.Background(), gitlabRepo, gitlabHookURL); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
	t.Run("delete refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.handle("GET "+glHooks, present)
		stub.json("DELETE "+glHooks+"/3", http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		if err := backend.DeleteWebhook(context.Background(), gitlabRepo, gitlabHookURL); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
	t.Run("read-back refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.sequence("GET "+glHooks, present, forgeJSON(http.StatusInternalServerError, map[string]any{"message": "500"}))
		stub.json("DELETE "+glHooks+"/3", http.StatusNoContent, nil)
		if err := backend.DeleteWebhook(context.Background(), gitlabRepo, gitlabHookURL); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
	t.Run("hook survives the delete", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.handle("GET "+glHooks, present)
		stub.json("DELETE "+glHooks+"/3", http.StatusNoContent, nil)
		if err := backend.DeleteWebhook(context.Background(), gitlabRepo, gitlabHookURL); !errors.Is(err, ErrForgeWriteNotApplied) {
			t.Fatalf("err = %v, want ErrForgeWriteNotApplied", err)
		}
	})
}

func TestGitLabBackend_RepoFiles_ListsTheDirectory(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glTree, http.StatusOK, "tree.json")

	files, err := backend.RepoFiles(context.Background(), gitlabRepo, " main ", "/.factory/")
	if err != nil {
		t.Fatalf("RepoFiles: %v", err)
	}
	want := []ForgeFile{
		{Path: ".factory/lanes", Name: "lanes", IsDir: true},
		{Path: ".factory/policy.yaml", Name: "policy.yaml"},
	}
	if !slices.Equal(files, want) {
		t.Fatalf("files = %+v, want %+v", files, want)
	}
	query := gitlabQuery(t, stub.requests("GET " + glTree)[0])
	if query.Get("path") != ".factory" || query.Get("ref") != "main" {
		t.Fatalf("tree query = %v", query)
	}
}

func TestGitLabBackend_RepoFiles_RootAndDefaultRef(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.handle("GET "+glTree, gitlabPaged(
		[]any{map[string]any{"name": "README.md", "path": "README.md", "type": "blob"}},
		[]any{map[string]any{"name": "pkg", "path": "pkg", "type": "tree"}},
	))

	files, err := backend.RepoFiles(context.Background(), gitlabRepo, "", "")
	if err != nil {
		t.Fatalf("RepoFiles: %v", err)
	}
	if len(files) != 2 || files[1].Path != "pkg" || !files[1].IsDir {
		t.Fatalf("files = %+v, want both pages", files)
	}
	query := gitlabQuery(t, stub.requests("GET " + glTree)[0])
	if query.Has("path") || query.Has("ref") {
		t.Fatalf("root at the default branch sent %v", query)
	}
}

func TestGitLabBackend_RepoFiles_AbsentDirectoryIsEmpty(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glTree, http.StatusNotFound, map[string]any{"message": "404 Tree Not Found"})

	files, err := backend.RepoFiles(context.Background(), gitlabRepo, "main", "missing")
	if err != nil {
		t.Fatalf("RepoFiles: %v", err)
	}
	if files == nil || len(files) != 0 {
		t.Fatalf("files = %#v, want an empty list", files)
	}
}

func TestGitLabBackend_RepoFiles_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glTree, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
	if _, err := backend.RepoFiles(context.Background(), gitlabRepo, "main", ".factory"); !errors.Is(err, ErrBackend) {
		t.Fatalf("err = %v, want ErrBackend", err)
	}
}

func TestGitLabBackend_BranchProtection_IsUnsupported(t *testing.T) {
	backend, stub := newGitLabStub(t)
	_, err := backend.BranchProtection(context.Background(), gitlabRepo, "main")
	var unsupported *ForgeUnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Type != ForgeGitLab {
		t.Fatalf("err = %v, want a GitLab ForgeUnsupportedError", err)
	}
	if calls := stub.writes(); len(calls) != 0 {
		t.Fatal("an unsupported read wrote to GitLab")
	}
}

func TestGitLabBackend_BranchProtection_Refusals(t *testing.T) {
	backend, _ := newGitLabStub(t)
	if _, err := backend.BranchProtection(context.Background(), gitlabRepo, " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}
