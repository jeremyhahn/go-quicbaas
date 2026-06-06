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

package grpc

import (
	"context"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

const sampleSpecYAML = "spec:\n  rules:\n    - name: r1\n      effect: allow\n      priority: 1\n"

func samplePolicy() *iampb.Policy {
	return &iampb.Policy{
		Name: "p1", TenantId: "t1", Description: "d", Priority: 1,
		Labels:    map[string]string{"k": "v"},
		SpecYaml:  []byte(sampleSpecYAML),
		CreatedAt: iamTime(), UpdatedAt: iamTime(),
	}
}

func sampleApp() *iampb.Application {
	return &iampb.Application{
		ClientId: "c1", ClientSecret: "s1", TenantId: "t1", Name: "app",
		RedirectUris: []string{"https://x"}, ResponseTypes: []string{"code"},
		GrantTypes: []string{"authorization_code"},
		CreatedAt:  iamTime(), UpdatedAt: iamTime(),
	}
}

// Policy methods.
func (f *fakeIAMServer) CreatePolicy(context.Context, *iampb.CreatePolicyRequest) (*iampb.Policy, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return samplePolicy(), nil
}
func (f *fakeIAMServer) GetPolicy(context.Context, *iampb.GetPolicyRequest) (*iampb.Policy, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return samplePolicy(), nil
}
func (f *fakeIAMServer) UpdatePolicy(context.Context, *iampb.UpdatePolicyRequest) (*iampb.Policy, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return samplePolicy(), nil
}
func (f *fakeIAMServer) DeletePolicy(context.Context, *iampb.DeletePolicyRequest) (*iampb.DeletePolicyResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DeletePolicyResponse{}, nil
}
func (f *fakeIAMServer) ListPolicies(context.Context, *iampb.ListPoliciesRequest) (*iampb.ListPoliciesResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ListPoliciesResponse{Policies: []*iampb.Policy{samplePolicy()}, Total: 1}, nil
}
func (f *fakeIAMServer) ValidatePolicy(context.Context, *iampb.ValidatePolicyRequest) (*iampb.ValidatePolicyResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ValidatePolicyResponse{Valid: false, Errors: []string{"bad rule"}}, nil
}
func (f *fakeIAMServer) TestPolicy(context.Context, *iampb.TestPolicyRequest) (*iampb.TestPolicyResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.TestPolicyResponse{Allowed: true, MatchedPolicy: "p1", MatchedRule: "r1", EvaluationTimeNs: 42}, nil
}

// App methods.
func (f *fakeIAMServer) RegisterApp(context.Context, *iampb.RegisterAppRequest) (*iampb.Application, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleApp(), nil
}
func (f *fakeIAMServer) GetApp(context.Context, *iampb.GetAppRequest) (*iampb.Application, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleApp(), nil
}
func (f *fakeIAMServer) UpdateApp(context.Context, *iampb.UpdateAppRequest) (*iampb.Application, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleApp(), nil
}
func (f *fakeIAMServer) DeleteApp(context.Context, *iampb.DeleteAppRequest) (*iampb.DeleteAppResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DeleteAppResponse{}, nil
}
func (f *fakeIAMServer) ListApps(context.Context, *iampb.ListAppsRequest) (*iampb.ListAppsResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ListAppsResponse{Apps: []*iampb.Application{sampleApp()}, Total: 1}, nil
}
func (f *fakeIAMServer) RotateAppSecret(context.Context, *iampb.RotateAppSecretRequest) (*iampb.Application, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleApp(), nil
}

var iamPolicyAppCases = []iamCase{
	{"CreatePolicy", "quicbaas.IAM.Policy", "Create", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"name": "p1", "spec_yaml": []byte(sampleSpecYAML)}}},
	{"GetPolicy", "quicbaas.IAM.Policy", "Get", map[string]interface{}{"tenant_id": "t1", "policy_name": "p1"}},
	{"UpdatePolicy", "quicbaas.IAM.Policy", "Update", map[string]interface{}{"tenant_id": "t1", "policy_name": "p1", "request": map[string]interface{}{"description": "d"}}},
	{"DeletePolicy", "quicbaas.IAM.Policy", "Delete", map[string]interface{}{"tenant_id": "t1", "policy_name": "p1"}},
	{"ListPolicies", "quicbaas.IAM.Policy", "List", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"page_size": 5}}},
	{"ValidatePolicy", "quicbaas.IAM.Policy", "Validate", map[string]interface{}{"tenant_id": "t1", "spec_yaml": []byte(sampleSpecYAML)}},
	{"TestPolicy", "quicbaas.IAM.Policy", "Test", map[string]interface{}{"tenant_id": "t1", "subject": map[string]interface{}{"id": "u1"}, "action": "read", "resource": "doc"}},
	{"SimulatePolicy", "quicbaas.IAM.Policy", "Simulate", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"subject": map[string]interface{}{"id": "u1"}, "action": "read", "resource": "doc"}}},
	{"RegisterApp", "quicbaas.IAM.App", "Register", map[string]interface{}{"tenant_id": "t1", "app": map[string]interface{}{"name": "app", "redirect_uris": []string{"https://x"}}}},
	{"GetApp", "quicbaas.IAM.App", "Get", map[string]interface{}{"tenant_id": "t1", "client_id": "c1"}},
	{"UpdateApp", "quicbaas.IAM.App", "Update", map[string]interface{}{"tenant_id": "t1", "client_id": "c1", "app": map[string]interface{}{"name": "app2"}}},
	{"DeleteApp", "quicbaas.IAM.App", "Delete", map[string]interface{}{"tenant_id": "t1", "client_id": "c1"}},
	{"ListApps", "quicbaas.IAM.App", "List", map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"page_size": 5}}},
	{"RotateAppSecret", "quicbaas.IAM.App", "RotateSecret", map[string]interface{}{"tenant_id": "t1", "client_id": "c1"}},
}

func init() {
	iamServiceCases = append(iamServiceCases, iamPolicyAppCases...)
}
