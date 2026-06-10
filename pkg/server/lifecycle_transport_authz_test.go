//go:build iam && quic && rest

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

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGRPCAuthzError_Format(t *testing.T) {
	assert.Equal(t, "grpc authz: op", (&GRPCAuthzError{Op: "op"}).Error())
	assert.Equal(t, "grpc authz: op: boom", (&GRPCAuthzError{Op: "op", Cause: errors.New("boom")}).Error())
}
