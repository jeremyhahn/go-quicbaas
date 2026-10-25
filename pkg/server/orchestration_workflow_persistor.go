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
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// workflowKeyPrefix is the KV key prefix under which all workflow
// definitions live. Keys are
//
//	_orchestration/workflows/<tenant>/<name>
//
// so a scan of one tenant's prefix can never observe another tenant's
// definitions, and two tenants may register the same name. The leading "_"
// pushes this namespace below user-visible entity types in any prefix listing.
const workflowKeyPrefix = "_orchestration/workflows/"

// workflowKeySeparator separates the tenant from the name inside a key. An
// identifier containing it is refused rather than encoded, because a crafted
// name could otherwise escape its tenant's subtree.
const workflowKeySeparator = "/"

// kvWorkflowPersistor stores workflow definitions in the QuicBaaS internal
// KV store via the qrdb SDK. All workflows for the cluster live on the
// configured shard (default 1) under workflowKeyPrefix.
type kvWorkflowPersistor struct {
	kv      transport.KVClient
	shardID uint64
}

// newKVWorkflowPersistor returns a persistor backed by the given qrdb KV
// client, which the server binds to its service principal.
func newKVWorkflowPersistor(kv transport.KVClient, shardID uint64) *kvWorkflowPersistor {
	return &kvWorkflowPersistor{kv: kv, shardID: shardID}
}

// workflowPersistor returns the persistor over shard's KV, run as the service
// principal: with the embedded node authorizing every call, an unbound
// persistor could neither save a workflow nor rehydrate one at boot.
func (s *Server) workflowPersistor(shard *qrdbsdk.Client) *kvWorkflowPersistor {
	return newKVWorkflowPersistor(s.serviceKV(shard.KV()), 1)
}

// workflowSegment refuses an identifier that cannot be a key segment.
func workflowSegment(field, value string) error {
	if value == "" {
		return &ServerMessageError{Message: "kvWorkflowPersistor: workflow " + field + " is required"}
	}
	if strings.Contains(value, workflowKeySeparator) {
		return &ServerMessageError{Message: "kvWorkflowPersistor: workflow " + field + " must not contain " + workflowKeySeparator}
	}
	return nil
}

// tenantPrefix returns the scan prefix covering one tenant's workflows.
func (p *kvWorkflowPersistor) tenantPrefix(tenantID string) (string, error) {
	if err := workflowSegment("tenant", tenantID); err != nil {
		return "", err
	}
	return workflowKeyPrefix + tenantID + workflowKeySeparator, nil
}

// keyFor returns the key holding one tenant's workflow.
func (p *kvWorkflowPersistor) keyFor(tenantID, name string) (string, error) {
	prefix, err := p.tenantPrefix(tenantID)
	if err != nil {
		return "", err
	}
	if err := workflowSegment("name", name); err != nil {
		return "", err
	}
	return prefix + name, nil
}

// Save serializes the definition as JSON and writes it under the tenant's
// workflow key. JSON is used (not YAML) because the rest of the orchestration
// layer already round-trips definitions as JSON over REST/gRPC.
func (p *kvWorkflowPersistor) Save(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	if def == nil {
		return &ServerMessageError{Message: "kvWorkflowPersistor: workflow name is required"}
	}
	key, err := p.keyFor(def.TenantID, def.Name)
	if err != nil {
		return err
	}
	data, err := json.Marshal(def)
	if err != nil {
		return &ServerCauseError{Message: fmt.Sprintf("kvWorkflowPersistor: marshal %q", def.Name), Cause: err}
	}
	if err := p.kv.Put(ctx, key, data); err != nil {
		return &ServerCauseError{Message: fmt.Sprintf("kvWorkflowPersistor: put %q", def.Name), Cause: err}
	}
	return nil
}

// Remove deletes the tenant's workflow entry. Idempotent — a missing key is
// not treated as an error since the loader has already validated that the
// in-memory entry existed.
func (p *kvWorkflowPersistor) Remove(ctx context.Context, tenantID, name string) error {
	key, err := p.keyFor(tenantID, name)
	if err != nil {
		return err
	}
	if err := p.kv.Delete(ctx, key); err != nil {
		return &ServerCauseError{Message: fmt.Sprintf("kvWorkflowPersistor: delete %q", name), Cause: err}
	}
	return nil
}

// Load scans the tenant's prefix and decodes each value back into a
// WorkflowDefinition.
func (p *kvWorkflowPersistor) Load(ctx context.Context, tenantID string) ([]*orchestration.WorkflowDefinition, error) {
	prefix, err := p.tenantPrefix(tenantID)
	if err != nil {
		return nil, err
	}
	return p.scan(ctx, prefix)
}

// LoadAll scans every key under workflowKeyPrefix and decodes each value
// back into a WorkflowDefinition. Called once at startup by RehydrateAll.
func (p *kvWorkflowPersistor) LoadAll(ctx context.Context) ([]*orchestration.WorkflowDefinition, error) {
	return p.scan(ctx, workflowKeyPrefix)
}

// scan decodes every workflow under prefix. The tenant and name are taken
// from the KEY, which is authoritative: a stored body that disagrees with
// where it is filed would otherwise let one tenant's record surface under
// another tenant's cache.
func (p *kvWorkflowPersistor) scan(ctx context.Context, prefix string) ([]*orchestration.WorkflowDefinition, error) {
	results, err := p.kv.Scan(ctx, prefix)
	if err != nil {
		return nil, &ServerCauseError{Message: "kvWorkflowPersistor: scan", Cause: err}
	}
	defs := make([]*orchestration.WorkflowDefinition, 0, len(results))
	for k, v := range results {
		tenantID, name, ok := splitWorkflowKey(k)
		if !ok {
			continue
		}
		def := &orchestration.WorkflowDefinition{}
		if err := json.Unmarshal(v, def); err != nil {
			// Skip malformed entries rather than abort the entire boot.
			// The orchestrator will simply not see this workflow until the
			// operator fixes or removes it via the API.
			continue
		}
		if def.Name == "" || def.Name != name {
			continue
		}
		def.TenantID = tenantID
		defs = append(defs, def)
	}
	return defs, nil
}

// splitWorkflowKey returns the tenant and name a workflow key addresses.
func splitWorkflowKey(key string) (tenantID, name string, ok bool) {
	rest, found := strings.CutPrefix(key, workflowKeyPrefix)
	if !found {
		return "", "", false
	}
	tenantID, name, found = strings.Cut(rest, workflowKeySeparator)
	if !found || tenantID == "" || name == "" || strings.Contains(name, workflowKeySeparator) {
		return "", "", false
	}
	return tenantID, name, true
}
