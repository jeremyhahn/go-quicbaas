//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCeremonyDispatch_NotConfigured verifies that the ceremony Handler
// sub-opcodes fail closed when no ceremony servicer is wired (requireCeremony
// Service returns an error response). This covers the not-configured branch of
// handlers such as handleCeremonyListOfficers and handleCeremonyStatus.
func TestCeremonyDispatch_NotConfigured(t *testing.T) {
	// No WithCeremonyService option -> ceremony service is nil.
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
	ctx := context.Background()

	cases := []struct {
		name  string
		subOp byte
		body  []byte
	}{
		{"list-officers", SubOpCeremonyListOfficers, nil},
		{"status", SubOpCeremonyStatus, nil},
		{"initialize", SubOpCeremonyInitialize, encMust(t, &ceremonyInitializeWireRequest{SOPin: "1", UserPin: "2", Threshold: 1})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, ceremonyRequest(tc.subOp, tc.body))
			assert.NotEqual(t, StatusOK, resp.Status, "%s must fail closed when ceremony not configured", tc.name)
		})
	}
}
