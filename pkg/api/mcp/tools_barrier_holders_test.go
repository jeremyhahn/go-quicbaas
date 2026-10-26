//go:build mcp

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

package mcp

import (
	"context"
	"encoding/base64"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errHolderBoom is a service failure the holder tools must pass through.
var errHolderBoom = stderrors.New("holder boom")

// fakeHolderBarrierService records the requests the barrier holder tools
// decode. Verbs it does not override panic through the nil embedded
// interface, so a tool routed to the wrong verb fails loudly.
type fakeHolderBarrierService struct {
	service.BarrierServicer

	err error

	gotRegister *qrdbtransport.OfficerRegisterRequest
	gotSubmit   qrdbtransport.QuorumSubmitRequest
	submitted   *qrdbtransport.QuorumSubmitRequest
}

func (f *fakeHolderBarrierService) OfficerRegister(_ context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	f.gotRegister = req
	if f.err != nil {
		return nil, f.err
	}
	return &qrdbtransport.OfficerRegisterResponse{CertPEM: "cert", Role: req.Role}, nil
}

func (f *fakeHolderBarrierService) BarrierQuorumSubmit(_ context.Context, req *qrdbtransport.QuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
	// Copy the secrets out: the tool wipes the request once this returns.
	f.gotSubmit = *req
	f.gotSubmit.Share = append([]byte(nil), req.Share...)
	f.gotSubmit.Response = append([]byte(nil), req.Response...)
	f.submitted = req
	if f.err != nil {
		return nil, f.err
	}
	return &qrdbtransport.QuorumOutcome{Domain: req.Domain, Op: req.Op, Required: 2, Submitted: 1}, nil
}

// fakeHolderTenantService is the tenant-barrier counterpart.
type fakeHolderTenantService struct {
	service.TenantBarrierServicer

	err         error
	officers    []*qrdbtransport.OfficerStatus
	gotTenantID string
}

func (f *fakeHolderTenantService) TenantBarrierOfficerList(_ context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error) {
	f.gotTenantID = tenantID
	if f.err != nil {
		return nil, f.err
	}
	return f.officers, nil
}

func runBarrierHolderTool(t *testing.T, svc service.BarrierServicer, name string, args map[string]interface{}) (interface{}, error) {
	t.Helper()
	tool, ok := barrierHolderTools[name]
	require.Truef(t, ok, "barrier holder tool %q not declared", name)
	return tool.exec(context.Background(), svc, args)
}

func TestBarrierHolderTool_QuorumSubmitDecodesAndWipes(t *testing.T) {
	share := []byte("share-bytes")
	response := []byte("factor-response")
	fake := &fakeHolderBarrierService{}

	out, err := runBarrierHolderTool(t, fake, "qrdb_barrier_quorum_submit", map[string]interface{}{
		"domain":            "unseal",
		"op":                "unseal",
		"username":          "alice",
		"challenge_id":      "ch-1",
		"factor":            "pin",
		"response":          base64.StdEncoding.EncodeToString(response),
		"response_metadata": `{"origin":"cli"}`,
		"share":             base64.StdEncoding.EncodeToString(share),
	})
	require.NoError(t, err)

	outcome, ok := out.(*qrdbtransport.QuorumOutcome)
	require.True(t, ok, "result is %T", out)
	assert.Equal(t, 1, outcome.Submitted)
	assert.Equal(t, "alice", fake.gotSubmit.Username)
	assert.Equal(t, "ch-1", fake.gotSubmit.ChallengeID)
	assert.Equal(t, share, fake.gotSubmit.Share)
	assert.Equal(t, response, fake.gotSubmit.Response)
	assert.Equal(t, map[string]string{"origin": "cli"}, fake.gotSubmit.ResponseMetadata)

	// The decoded share and factor response must not outlive the call.
	assert.Equal(t, make([]byte, len(share)), fake.submitted.Share)
	assert.Equal(t, make([]byte, len(response)), fake.submitted.Response)
}

func TestBarrierHolderTool_QuorumSubmitPassesServiceError(t *testing.T) {
	fake := &fakeHolderBarrierService{err: errHolderBoom}
	_, err := runBarrierHolderTool(t, fake, "qrdb_barrier_quorum_submit", map[string]interface{}{
		"domain": "unseal",
		"share":  base64.StdEncoding.EncodeToString([]byte("s")),
	})
	require.ErrorIs(t, err, errHolderBoom)
	assert.Equal(t, make([]byte, 1), fake.submitted.Share, "the share is wiped on failure too")
}

// TestHolderVerbInto_PartialDecodeLeavesShareForTheWipe pins Codex 7395 P2:
// a submission whose share decodes and whose username does not is refused
// with the share already populated, so the submit tools register
// wipeQuorumSubmission before the decode, and it zeroes what the decode left.
func TestHolderVerbInto_PartialDecodeLeavesShareForTheWipe(t *testing.T) {
	called := false
	submit := func(context.Context, *qrdbtransport.QuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
		called = true
		return &qrdbtransport.QuorumOutcome{}, nil
	}
	var req qrdbtransport.QuorumSubmitRequest
	_, err := holderVerbInto(context.Background(), map[string]interface{}{
		"share":    base64.StdEncoding.EncodeToString([]byte("hunter2")),
		"username": 42,
	}, &req, submit)
	require.Error(t, err)
	require.False(t, called, "the service must not see an undecodable submission")
	require.Equal(t, []byte("hunter2"), req.Share, "precondition: a partial decode populates the share")

	wipeQuorumSubmission(&req.Share, &req.Response)
	assert.Equal(t, make([]byte, len("hunter2")), req.Share)
}

func TestBarrierHolderTool_OfficerRegisterParsesFactors(t *testing.T) {
	fake := &fakeHolderBarrierService{}
	out, err := runBarrierHolderTool(t, fake, "qrdb_barrier_officer_register", map[string]interface{}{
		"username": "bob",
		"role":     "custodian",
		"csr_pem":  "csr",
		"factors":  `[{"name":"pin"}]`,
	})
	require.NoError(t, err)

	resp, ok := out.(*qrdbtransport.OfficerRegisterResponse)
	require.True(t, ok, "result is %T", out)
	assert.Equal(t, "custodian", resp.Role)
	require.NotNil(t, fake.gotRegister)
	assert.Equal(t, "bob", fake.gotRegister.Username)
	assert.Equal(t, []qrdbtransport.HolderFactorEnrolment{{Name: "pin"}}, fake.gotRegister.Factors)
}

func TestBarrierHolderTool_EmptyJSONFieldIsOmitted(t *testing.T) {
	fake := &fakeHolderBarrierService{}
	_, err := runBarrierHolderTool(t, fake, "qrdb_barrier_officer_register", map[string]interface{}{
		"username": "bob",
		"factors":  "",
	})
	require.NoError(t, err)
	assert.Nil(t, fake.gotRegister.Factors)
}

func TestBarrierHolderTool_RejectsBadArguments(t *testing.T) {
	cases := []struct {
		name      string
		args      map[string]interface{}
		wantField string
	}{
		{"malformed JSON field", map[string]interface{}{"username": "bob", "factors": "[{"}, "factors"},
		{"wrong argument type", map[string]interface{}{"username": 7}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeHolderBarrierService{}
			_, err := runBarrierHolderTool(t, fake, "qrdb_barrier_officer_register", c.args)
			require.Error(t, err)

			var qbErr *qberrors.QBError
			require.True(t, stderrors.As(err, &qbErr), "expected a QBError, got %T", err)
			assert.Equal(t, qberrors.ErrInvalidInput, qbErr.Code)
			var argsErr *HolderArgsError
			require.True(t, stderrors.As(err, &argsErr), "expected a HolderArgsError, got %T", err)
			assert.Equal(t, c.wantField, argsErr.Field)
			assert.Nil(t, fake.gotRegister, "the service must not see undecodable arguments")
		})
	}
}

func TestHolderArgsError_Message(t *testing.T) {
	inner := stderrors.New("bad")
	assert.Equal(t, "holder tool argument factors: bad", (&HolderArgsError{Field: "factors", Err: inner}).Error())
	assert.Equal(t, "holder tool arguments: bad", (&HolderArgsError{Err: inner}).Error())
	assert.ErrorIs(t, &HolderArgsError{Err: inner}, inner)
}

func TestTenantHolderTool_OfficerList(t *testing.T) {
	tool := tenantBarrierHolderTools["qrdb_tenant_barrier_officer_list"]
	fake := &fakeHolderTenantService{officers: []*qrdbtransport.OfficerStatus{{Username: "alice", ShareIndex: 1}}}

	out, err := tool.exec(context.Background(), fake, map[string]interface{}{"tenant_id": "t1"})
	require.NoError(t, err)
	assert.Equal(t, "t1", fake.gotTenantID)
	assert.Equal(t, holderOfficers{Officers: fake.officers}, out)
}

func TestTenantHolderTool_OfficerListErrors(t *testing.T) {
	tool := tenantBarrierHolderTools["qrdb_tenant_barrier_officer_list"]

	t.Run("missing tenant_id", func(t *testing.T) {
		fake := &fakeHolderTenantService{}
		_, err := tool.exec(context.Background(), fake, map[string]interface{}{})
		var want *TenantBarrierTenantIDRequiredError
		require.True(t, stderrors.As(err, &want), "got %v", err)
		assert.Empty(t, fake.gotTenantID)
	})
	t.Run("service error", func(t *testing.T) {
		fake := &fakeHolderTenantService{err: errHolderBoom}
		_, err := tool.exec(context.Background(), fake, map[string]interface{}{"tenant_id": "t1"})
		require.ErrorIs(t, err, errHolderBoom)
	})
}

func TestHolderSchema_DeclaresRequiredArguments(t *testing.T) {
	schema := holderSchema([]holderArg{
		{name: "username", kind: "string", description: "u", required: true},
		{name: "role", kind: "string", description: "r"},
	})
	assert.Equal(t, []string{"username"}, schema["required"])
	properties, ok := schema["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Len(t, properties, 2)

	_, hasRequired := holderSchema([]holderArg{{name: "role", kind: "string"}})["required"]
	assert.False(t, hasRequired, "a schema with no required argument omits the key")
}

// TestHolderTools_RoutedThroughSurfaces pins that every holder tool is in its
// surface's dispatch table, so the surface's unwired-service guard and authz
// mapping both apply to it.
func TestHolderTools_RoutedThroughSurfaces(t *testing.T) {
	for name := range barrierHolderTools {
		assert.Containsf(t, barrierToolNames, name, "barrier holder tool %q", name)
		assert.Containsf(t, barrierExecutors, name, "barrier holder tool %q", name)
	}
	for name := range tenantBarrierHolderTools {
		assert.Containsf(t, tenantBarrierToolNames, name, "tenant holder tool %q", name)
		assert.Containsf(t, tenantBarrierExecutors, name, "tenant holder tool %q", name)
	}
}
