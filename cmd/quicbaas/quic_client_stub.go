//go:build !quic || !rest

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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	stderrors "errors"
)

// ErrQUICNotCompiled is returned when QUIC functions are called without the quic build tag.
var ErrQUICNotCompiled = stderrors.New("quic support not compiled - build with -tags quic")

func createQUICSecretsClient() (SecretsClient, error) {
	return nil, ErrQUICNotCompiled
}
