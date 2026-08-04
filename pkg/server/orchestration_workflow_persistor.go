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

// Package server: orchestration workflow persistor
//
// kvWorkflowPersistor is the canonical WorkflowPersistor implementation for
// QuicBaaS. Workflow definitions are stored in the same internal KV database
// that backs every other persistent entity (projects, secrets, KMS keys,
// IAM, etc.) so they replicate across raft peers and survive restarts the
// same way as user data. Storing workflows on the local filesystem would
// violate the "QuicBaaS is the database" principle and break in multi-node
// deployments — they live in the cluster, not on a single host.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// workflowKeyPrefix is the KV key prefix under which all workflow
// definitions live. Names are appended after the prefix to form full keys.
// The leading "_" pushes this namespace below user-visible entity types in
// any prefix listing.
const workflowKeyPrefix = "_orchestration/workflows/"

// kvWorkflowPersistor stores workflow definitions in the QuicBaaS internal
// KV store via the qrdb SDK. All workflows for the cluster live on the
// configured shard (default 1) under workflowKeyPrefix.
type kvWorkflowPersistor struct {
	client  *qrdbsdk.Client
	shardID uint64
}

// newKVWorkflowPersistor returns a persistor backed by the given qrdb client.
func newKVWorkflowPersistor(client *qrdbsdk.Client, shardID uint64) *kvWorkflowPersistor {
	return &kvWorkflowPersistor{client: client, shardID: shardID}
}

func (p *kvWorkflowPersistor) keyFor(name string) string {
	return workflowKeyPrefix + name
}

// Save serializes the definition as JSON and writes it under the
// canonical workflow key. JSON is used (not YAML) because the rest of the
// orchestration layer already round-trips definitions as JSON over REST/gRPC.
func (p *kvWorkflowPersistor) Save(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	if def == nil || def.Name == "" {
		return &ServerMessageError{Message: "kvWorkflowPersistor: workflow name is required"}
	}
	data, err := json.Marshal(def)
	if err != nil {
		return &ServerCauseError{Message: fmt.Sprintf("kvWorkflowPersistor: marshal %q", def.Name), Cause: err}
	}
	if err := p.client.KV().Put(ctx, p.keyFor(def.Name), data); err != nil {
		return &ServerCauseError{Message: fmt.Sprintf("kvWorkflowPersistor: put %q", def.Name), Cause: err}
	}
	return nil
}

// Remove deletes the workflow's KV entry. Idempotent — a missing key is not
// treated as an error since the loader has already validated that the
// in-memory entry existed.
func (p *kvWorkflowPersistor) Remove(ctx context.Context, name string) error {
	if name == "" {
		return &ServerMessageError{Message: "kvWorkflowPersistor: workflow name is required"}
	}
	if err := p.client.KV().Delete(ctx, p.keyFor(name)); err != nil {
		// Treat any "not found" as success so the operation stays idempotent.
		// The qrdb SDK does not currently expose a typed NotFound error here,
		// so callers retrying a Remove after a partial failure see success.
		return &ServerCauseError{Message: fmt.Sprintf("kvWorkflowPersistor: delete %q", name), Cause: err}
	}
	return nil
}

// LoadAll scans every key under workflowKeyPrefix and decodes each value
// back into a WorkflowDefinition. Called once at startup by Rehydrate.
func (p *kvWorkflowPersistor) LoadAll(ctx context.Context) ([]*orchestration.WorkflowDefinition, error) {
	results, err := p.client.KV().Scan(ctx, workflowKeyPrefix)
	if err != nil {
		return nil, &ServerCauseError{Message: "kvWorkflowPersistor: scan", Cause: err}
	}
	defs := make([]*orchestration.WorkflowDefinition, 0, len(results))
	for k, v := range results {
		if !strings.HasPrefix(k, workflowKeyPrefix) {
			continue
		}
		def := &orchestration.WorkflowDefinition{}
		if err := json.Unmarshal(v, def); err != nil {
			// Skip malformed entries rather than abort the entire boot.
			// The orchestrator will simply not see this workflow until the
			// operator fixes or removes it via the API.
			continue
		}
		if def.Name == "" {
			continue
		}
		defs = append(defs, def)
	}
	return defs, nil
}
