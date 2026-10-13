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
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// TestHandleIAMUserCreate_MalformedEmailIsInvalidInput proves an in-process
// SDK caller can classify a refused field as invalid input, the same class the
// wire transports answer 400 / InvalidArgument for (go-quicbaas#335).
func TestHandleIAMUserCreate_MalformedEmailIsInvalidInput(t *testing.T) {
	svc, _ := newIAMTestService()

	_, err := handleIAMUserCreate(context.Background(), svc, &embeddedCreateUserEnvelope{
		TenantID: "t-1",
		Request:  &iamsdk.CreateUserRequest{Username: "pat", Email: "not-an-email"},
	})
	if err == nil {
		t.Fatal("a malformed email was accepted")
	}
	var coded *dberrors.QBError
	if !errors.As(err, &coded) {
		t.Fatalf("want a coded error, got %T: %v", err, err)
	}
	if coded.Code != dberrors.ErrInvalidInput {
		t.Errorf("error code = %v, want ErrInvalidInput", coded.Code)
	}
	var verr *iam.ValidationError
	if !errors.As(err, &verr) || verr.Field != "email" {
		t.Errorf("want *iam.ValidationError for field email, got %v", err)
	}
}
