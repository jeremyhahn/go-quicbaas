// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"bytes"
	"errors"
	"testing"
	"testing/iotest"
)

func TestNewWorkflowAgentID_Format(t *testing.T) {
	r := bytes.NewReader([]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef})
	got, err := newWorkflowAgentID(r, "worker")
	if err != nil {
		t.Fatalf("newWorkflowAgentID: %v", err)
	}
	if want := "wf-worker-0123456789abcdef"; got != want {
		t.Fatalf("newWorkflowAgentID = %q, want %q", got, want)
	}
}

func TestNewWorkflowAgentID_RandomSourceFailure(t *testing.T) {
	cause := errors.New("entropy exhausted")
	_, err := newWorkflowAgentID(iotest.ErrReader(cause), "worker")

	var idErr *WorkflowAgentIDError
	if !errors.As(err, &idErr) {
		t.Fatalf("error = %v, want *WorkflowAgentIDError", err)
	}
	if idErr.Role != "worker" {
		t.Fatalf("Role = %q, want worker", idErr.Role)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error does not wrap the random-source failure: %v", err)
	}
}
