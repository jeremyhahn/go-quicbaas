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

package quic

import (
	"context"
	"net/http"
	"testing"

	sdlcclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestRequest_OperatorVerbsOverHTTP3(t *testing.T) {
	type hit struct{ method, path string }
	hits := make(chan hit, 2)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- hit{r.Method, r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":88,"title":"Follow-up"}`))
			return
		}
		_, _ = w.Write([]byte(`{"users":[{"login":"acme"}],"count":1}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	ctx := context.Background()

	resp, err := tr.Request(ctx, "quicbaas.SDLC", "FileIssue", &sdlcclient.FileIssueRequest{ProjectID: "p1", Title: "Follow-up"})
	if err != nil || resp.(*sdlcclient.ForgeIssue).Number != 88 {
		t.Fatalf("FileIssue: %+v %v", resp, err)
	}
	if got := <-hits; got.method != http.MethodPost || got.path != "/api/v1/sdlc/projects/p1/issues" {
		t.Fatalf("FileIssue hit %+v", got)
	}
	resp, err = tr.Request(ctx, "quicbaas.SDLC", "Collaborators", &sdlcclient.CollaboratorsRequest{ProjectID: "p1"})
	if err != nil || resp.(*sdlcclient.CollaboratorsResponse).Count != 1 {
		t.Fatalf("Collaborators: %+v %v", resp, err)
	}
	if got := <-hits; got.method != http.MethodGet || got.path != "/api/v1/sdlc/projects/p1/collaborators" {
		t.Fatalf("Collaborators hit %+v", got)
	}
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "FileIssue", "wrong"); err == nil {
		t.Fatal("the wrong request type was accepted")
	}
}
