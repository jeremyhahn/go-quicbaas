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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// TestAccountEmbedded_SecretRefRoundTrips proves the in-process transport
// carries a credential_secret_ref in on create and update, and reports it back
// on the account.
func TestAccountEmbedded_SecretRefRoundTrips(t *testing.T) {
	const ref = "agents/codex/reviewer/auth_json"
	const file = ".codex/auth.json"
	var createReq *service.CreateAccountRequest
	var updateReq *service.UpdateAccountRequest
	referenced := embeddedSampleAccount()
	referenced.CredentialSecretRef = ref
	referenced.CredentialSecretFile = file

	tr := newAccountTransport(t, &mocks.MockAccountService{
		CreateAccountFunc: func(ctx context.Context, req *service.CreateAccountRequest) (*service.CreateAccountResponse, error) {
			createReq = req
			return &service.CreateAccountResponse{Account: referenced}, nil
		},
		UpdateAccountFunc: func(ctx context.Context, req *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			updateReq = req
			return &service.UpdateAccountResponse{Account: referenced}, nil
		},
	})
	ctx := context.Background()

	created, err := tr.Request(ctx, accountServiceName, "Create", &accountSDK.CreateAccountRequest{
		TenantID: "tenant-1", Provider: accountSDK.ProviderCodex,
		CredentialSecretRef: ref, CredentialSecretFile: file,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if createReq == nil || createReq.Credential != nil ||
		createReq.CredentialSecretRef != ref || createReq.CredentialSecretFile != file {
		t.Fatalf("reference lost in conversion: %+v", createReq)
	}
	info, ok := created.(*accountSDK.AccountInfo)
	if !ok || info.CredentialSecretRef != ref || info.CredentialSecretFile != file {
		t.Fatalf("reference not reported back: %#v", created)
	}

	if _, err := tr.Request(ctx, accountServiceName, "Update", &accountSDK.UpdateAccountRequest{
		TenantID: "tenant-1", AccountID: "acct-1", CredentialSecretRef: ref,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updateReq == nil || updateReq.CredentialSecretRef != ref {
		t.Fatalf("update reference lost in conversion: %+v", updateReq)
	}
}
