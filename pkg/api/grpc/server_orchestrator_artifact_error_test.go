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

package grpc

import (
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestConvertArtifactError(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		expected codes.Code
	}{
		{"artifact_not_found", orchestration.ErrArtifactNotFound, codes.NotFound},
		{"artifact_kind_invalid", orchestration.ErrArtifactKindInvalid, codes.InvalidArgument},
		{"artifact_ref_invalid", orchestration.ErrArtifactRefInvalid, codes.InvalidArgument},
		{"artifact_too_large", orchestration.ErrArtifactTooLarge, codes.ResourceExhausted},
		{"task_not_found", orchestration.ErrTaskNotFound, codes.NotFound},
		{"wrapped_not_found", stderrors.Join(orchestration.ErrArtifactNotFound, stderrors.New("ctx")), codes.NotFound},
		{"unknown_internal", stderrors.New("some other error"), codes.Internal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := convertArtifactError(tt.input)
			st, ok := status.FromError(err)
			assert.True(t, ok)
			assert.Equal(t, tt.expected, st.Code())
		})
	}
}
