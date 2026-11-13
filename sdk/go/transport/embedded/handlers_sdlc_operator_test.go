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

package embedded

import (
	"context"
	"reflect"
	"testing"

	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCEmbedded_OperatorVerbsRefuseWithoutTheModuleAndOnBadTypes(t *testing.T) {
	ctx := context.Background()
	for method, req := range map[string]interface{}{
		"FileIssue":     &sdlc.FileIssueRequest{ProjectID: "p", Title: "t"},
		"Collaborators": &sdlc.CollaboratorsRequest{ProjectID: "p"},
	} {
		handler := unaryHandlers["quicbaas.SDLC/"+method]
		if handler == nil {
			t.Fatalf("%s is not routed", method)
		}
		if _, err := handler(ctx, sdlcNilFacade{}, req); err != pkgsdlc.ErrUnavailable {
			t.Fatalf("%s without the module: %v", method, err)
		}
		if _, err := handler(ctx, sdlcNilFacade{}, "wrong"); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
}

func TestSDLCEmbedded_RulersSurviveTheInProcessHop(t *testing.T) {
	sdk := &sdlc.ProjectBinding{ProjectID: "p1", Rulers: []string{"lead", "ops"}}
	domain := sdlcBindingToDomain(sdk)
	if !reflect.DeepEqual(domain.Rulers, sdk.Rulers) {
		t.Fatalf("to domain: %v", domain.Rulers)
	}
	domain.Rulers[0] = "mallory"
	if sdk.Rulers[0] != "lead" {
		t.Fatal("the domain record aliases the SDK's rulers")
	}
	if back := sdlcBindingFromDomain(&pkgsdlc.ProjectBinding{ProjectID: "p1", Rulers: []string{"ops"}}); !reflect.DeepEqual(back.Rulers, []string{"ops"}) {
		t.Fatalf("from domain: %v", back.Rulers)
	}
}
