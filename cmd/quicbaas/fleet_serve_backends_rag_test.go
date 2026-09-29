//go:build rag

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

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestBuildAgentBackends_OpencodeListsItsNodesOllamaModels: the opencode
// catalog a node reports includes what its own ollama serves, as
// ollama/<name>, read from ollama when asked. The ollama client lists models
// only in a rag build, which is what every shipped binary is (WITH_RAG=1).
func TestBuildAgentBackends_OpencodeListsItsNodesOllamaModels(t *testing.T) {
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"review-q8:latest","model":"review-q8:latest"}]}`))
	}))
	defer ollamaServer.Close()

	cfg := &fleet.Config{
		Backends:      []fleet.BackendKind{fleet.BackendOpencode, fleet.BackendOllama},
		OllamaBaseURL: ollamaServer.URL,
	}
	backends, err := buildFleetBackends(cfg, newFleetServeLogger())
	require.NoError(t, err)
	models, err := backends["opencode"].(orchestration.ModelLister).ListModels(context.Background())
	require.NoError(t, err)
	assert.Contains(t, models, "ollama/review-q8:latest")
	assert.Contains(t, models, "openai/gpt-5.5", "the curated hosted models stay listed")
}
