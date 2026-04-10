//go:build quic && !iam

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

package quic

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestTryHandleIAMRequest_ReturnsNilStub(t *testing.T) {
	logCfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-stub-test",
	}
	log, _ := logger.New(logCfg)
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, log)

	req := &Request{Opcode: 0xFF, Payload: []byte{}}
	result := handler.tryHandleIAMRequest(context.Background(), req)

	if result != nil {
		t.Errorf("tryHandleIAMRequest should return nil when IAM is not enabled, got %v", result)
	}
}

func TestExtendOpcodeToString_ReturnsEmptyStub(t *testing.T) {
	result := extendOpcodeToString(0xFF)
	if result != "" {
		t.Errorf("extendOpcodeToString should return empty string when IAM is not enabled, got %q", result)
	}
}
