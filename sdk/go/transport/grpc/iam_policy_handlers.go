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
	"strings"

	"gopkg.in/yaml.v3"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// handleCreatePolicy handles CreatePolicy requests.
func (h *iamHandler) handleCreatePolicy(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCreatePolicy", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	requestData, _ := getMap(reqMap, "request")

	name, _ := getString(requestData, "name")
	description, _ := getString(requestData, "description")
	labels, _ := getStringMap(requestData, "labels")
	priority, _ := getInt(requestData, "priority")
	specYAML, _ := getBytes(requestData, "spec_yaml")

	pbReq := &iampb.CreatePolicyRequest{
		TenantId:    tenantID,
		Name:        name,
		Description: description,
		Labels:      labels,
		Priority:    int32(priority),
		SpecYaml:    specYAML,
	}

	resp, err := client.CreatePolicy(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCreatePolicy")
	}

	return convertProtoPolicyToSDK(resp), nil
}

// handleGetPolicy handles GetPolicy requests.
func (h *iamHandler) handleGetPolicy(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetPolicy", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	policyName, _ := getString(reqMap, "policy_name")

	pbReq := &iampb.GetPolicyRequest{
		TenantId:   tenantID,
		PolicyName: policyName,
	}

	resp, err := client.GetPolicy(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetPolicy")
	}

	return convertProtoPolicyToSDK(resp), nil
}

// handleUpdatePolicy handles UpdatePolicy requests.
func (h *iamHandler) handleUpdatePolicy(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleUpdatePolicy", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	policyName, _ := getString(reqMap, "policy_name")
	requestData, _ := getMap(reqMap, "request")

	description, _ := getString(requestData, "description")
	labels, _ := getStringMap(requestData, "labels")
	priority, _ := getInt(requestData, "priority")
	specYAML, _ := getBytes(requestData, "spec_yaml")

	pbReq := &iampb.UpdatePolicyRequest{
		TenantId:    tenantID,
		PolicyName:  policyName,
		Description: description,
		Labels:      labels,
		Priority:    int32(priority),
		SpecYaml:    specYAML,
	}

	resp, err := client.UpdatePolicy(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleUpdatePolicy")
	}

	return convertProtoPolicyToSDK(resp), nil
}

// handleDeletePolicy handles DeletePolicy requests.
func (h *iamHandler) handleDeletePolicy(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDeletePolicy", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	policyName, _ := getString(reqMap, "policy_name")

	pbReq := &iampb.DeletePolicyRequest{
		TenantId:   tenantID,
		PolicyName: policyName,
	}

	_, err := client.DeletePolicy(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleDeletePolicy")
	}

	// Return nil to indicate success - the SDK client accepts nil response.
	return nil, nil
}

// handleListPolicies handles ListPolicies requests.
func (h *iamHandler) handleListPolicies(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListPolicies", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	requestData, _ := getMap(reqMap, "request")

	pageSize, _ := getInt(requestData, "page_size")

	pbReq := &iampb.ListPoliciesRequest{
		TenantId: tenantID,
		Limit:    int32(pageSize),
	}

	resp, err := client.ListPolicies(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListPolicies")
	}

	policies := make([]iam.Policy, 0, len(resp.Policies))
	for _, p := range resp.Policies {
		if converted := convertProtoPolicyToSDK(p); converted != nil {
			policies = append(policies, *converted)
		}
	}

	return &iam.ListPoliciesResponse{
		Policies:   policies,
		TotalCount: int64(resp.Total),
	}, nil
}

// handleValidatePolicy handles ValidatePolicy requests.
func (h *iamHandler) handleValidatePolicy(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleValidatePolicy", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")

	// Try to get spec_yaml directly.
	specYAML, _ := getBytes(reqMap, "spec_yaml")

	// If not found at root level, check in the policy object.
	if len(specYAML) == 0 {
		if policyMap, ok := getMap(reqMap, "policy"); ok {
			// Try to get spec_yaml from the policy object.
			specYAML, _ = getBytes(policyMap, "spec_yaml")

			// Set tenant_id from policy if not already set.
			if tenantID == "" {
				tenantID, _ = getString(policyMap, "tenant_id")
			}
		}
	}

	pbReq := &iampb.ValidatePolicyRequest{
		TenantId: tenantID,
		SpecYaml: specYAML,
	}

	resp, err := client.ValidatePolicy(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleValidatePolicy")
	}

	var validationErrors []iam.ValidationError
	if resp.Errors != nil {
		for _, errStr := range resp.Errors {
			validationErrors = append(validationErrors, iam.ValidationError{
				Message: errStr,
			})
		}
	}

	return &iam.ValidationResult{
		Valid:  resp.Valid,
		Errors: validationErrors,
	}, nil
}

// handleTestPolicy handles TestPolicy requests.
func (h *iamHandler) handleTestPolicy(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleTestPolicy", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	subjectMap, _ := getMap(reqMap, "subject")
	action, _ := getString(reqMap, "action")
	resource, _ := getString(reqMap, "resource")
	contextMap, _ := getStringMap(reqMap, "context")

	var subject *iampb.Subject
	if subjectMap != nil {
		subject = extractSubjectFromMap(subjectMap)
	}

	pbReq := &iampb.TestPolicyRequest{
		TenantId: tenantID,
		Subject:  subject,
		Action:   action,
		Resource: resource,
		Context:  contextMap,
	}

	resp, err := client.TestPolicy(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleTestPolicy")
	}

	// Map proto TestPolicy response to SDK TestPolicyResponse.
	// The proto response represents a single test case evaluation.
	passed := 0
	failed := 0
	var actualEffect iam.Effect
	if resp.Allowed {
		passed = 1
		actualEffect = iam.EffectAllow
	} else {
		failed = 1
		actualEffect = iam.EffectDeny
	}

	testResult := iam.TestCaseResult{
		Name:             "policy_test",
		Passed:           resp.Allowed,
		ActualResult:     actualEffect,
		MatchedRule:      resp.MatchedRule,
		EvaluationTimeNs: resp.EvaluationTimeNs,
	}

	return &iam.TestPolicyResponse{
		Passed:  passed,
		Failed:  failed,
		Results: []iam.TestCaseResult{testResult},
	}, nil
}

// handleSimulatePolicy handles SimulatePolicy requests by mapping to TestPolicy RPC.
// The SDK's Simulate method maps to the proto's TestPolicy RPC.
func (h *iamHandler) handleSimulatePolicy(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleSimulatePolicy", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")

	// The request may be nested under "request" key from SDK.
	requestData, ok := getMap(reqMap, "request")
	if !ok {
		requestData = reqMap
	}

	subjectMap, _ := getMap(requestData, "subject")
	action, _ := getString(requestData, "action")
	resource, _ := getString(requestData, "resource")
	contextMap, _ := getStringMap(requestData, "context")

	var subject *iampb.Subject
	if subjectMap != nil {
		subject = extractSubjectFromMap(subjectMap)
	}

	pbReq := &iampb.TestPolicyRequest{
		TenantId: tenantID,
		Subject:  subject,
		Action:   action,
		Resource: resource,
		Context:  contextMap,
	}

	resp, err := client.TestPolicy(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleSimulatePolicy")
	}

	effect := iam.EffectDeny
	if resp.Allowed {
		effect = iam.EffectAllow
	}

	var matchedPolicies []string
	if resp.MatchedPolicy != "" {
		matchedPolicies = []string{resp.MatchedPolicy}
	}

	var matchedRules []string
	if resp.MatchedRule != "" {
		matchedRules = []string{resp.MatchedRule}
	}

	return &iam.SimulateResponse{
		Allowed:          resp.Allowed,
		Effect:           effect,
		MatchedPolicies:  matchedPolicies,
		MatchedRules:     matchedRules,
		EvaluationTimeNs: resp.EvaluationTimeNs,
	}, nil
}

// handleRegisterApp handles RegisterApp requests.
func (h *iamHandler) handleRegisterApp(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleRegisterApp", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	// SDK sends data under "app" key, check both for compatibility.
	requestData, ok := getMap(reqMap, "app")
	if !ok {
		requestData, _ = getMap(reqMap, "request")
	}

	name, _ := getString(requestData, "name")
	redirectURIs, _ := getStringSlice(requestData, "redirect_uris")
	resourcePrefix, _ := getString(requestData, "resource_prefix")
	grantTypes, _ := getStringSlice(requestData, "grant_types")
	responseTypes, _ := getStringSlice(requestData, "response_types")

	pbReq := &iampb.RegisterAppRequest{
		TenantId:       tenantID,
		Name:           name,
		RedirectUris:   redirectURIs,
		ResourcePrefix: resourcePrefix,
		GrantTypes:     grantTypes,
		ResponseTypes:  responseTypes,
	}

	resp, err := client.RegisterApp(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleRegisterApp")
	}

	return convertProtoAppToSDK(resp), nil
}

// handleGetApp handles GetApp requests.
func (h *iamHandler) handleGetApp(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetApp", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	clientID, _ := getString(reqMap, "client_id")

	pbReq := &iampb.GetAppRequest{
		TenantId: tenantID,
		ClientId: clientID,
	}

	resp, err := client.GetApp(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetApp")
	}

	return convertProtoAppToSDK(resp), nil
}

// handleUpdateApp handles UpdateApp requests.
func (h *iamHandler) handleUpdateApp(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleUpdateApp", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	clientID, _ := getString(reqMap, "client_id")
	// SDK sends data under "app" key, check both for compatibility.
	requestData, ok := getMap(reqMap, "app")
	if !ok {
		requestData, _ = getMap(reqMap, "request")
	}

	name, _ := getString(requestData, "name")
	redirectURIs, _ := getStringSlice(requestData, "redirect_uris")
	resourcePrefix, _ := getString(requestData, "resource_prefix")
	grantTypes, _ := getStringSlice(requestData, "grant_types")
	responseTypes, _ := getStringSlice(requestData, "response_types")

	pbReq := &iampb.UpdateAppRequest{
		TenantId:       tenantID,
		ClientId:       clientID,
		Name:           name,
		RedirectUris:   redirectURIs,
		ResourcePrefix: resourcePrefix,
		GrantTypes:     grantTypes,
		ResponseTypes:  responseTypes,
	}

	resp, err := client.UpdateApp(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleUpdateApp")
	}

	return convertProtoAppToSDK(resp), nil
}

// handleDeleteApp handles DeleteApp requests.
func (h *iamHandler) handleDeleteApp(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDeleteApp", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	clientID, _ := getString(reqMap, "client_id")

	pbReq := &iampb.DeleteAppRequest{
		TenantId: tenantID,
		ClientId: clientID,
	}

	_, err := client.DeleteApp(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleDeleteApp")
	}

	// Return nil to indicate success - the SDK client accepts nil response.
	return nil, nil
}

// handleListApps handles ListApps requests.
func (h *iamHandler) handleListApps(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListApps", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	requestData, _ := getMap(reqMap, "request")

	pageSize, _ := getInt(requestData, "page_size")

	pbReq := &iampb.ListAppsRequest{
		TenantId: tenantID,
		Limit:    int32(pageSize),
	}

	resp, err := client.ListApps(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListApps")
	}

	apps := make([]iam.Application, 0, len(resp.Apps))
	for _, a := range resp.Apps {
		if converted := convertProtoAppToSDK(a); converted != nil {
			apps = append(apps, *converted)
		}
	}

	return &iam.ListAppsResponse{
		Applications: apps,
		TotalCount:   int(resp.Total),
	}, nil
}

// handleRotateAppSecret handles RotateAppSecret requests.
func (h *iamHandler) handleRotateAppSecret(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleRotateAppSecret", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	clientID, _ := getString(reqMap, "client_id")

	pbReq := &iampb.RotateAppSecretRequest{
		TenantId: tenantID,
		ClientId: clientID,
	}

	resp, err := client.RotateAppSecret(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleRotateAppSecret")
	}

	app := convertProtoAppToSDK(resp)
	if app == nil {
		return nil, errors.NewInternal("handleRotateAppSecret", "nil response from server")
	}

	return &iam.RotateSecretResponse{
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret,
	}, nil
}

// policySpecYAML represents the top-level YAML policy spec structure
// used to extract rules from the SpecYaml field in the proto response.
type policySpecYAML struct {
	Spec struct {
		Rules []specRuleYAML `yaml:"rules"`
	} `yaml:"spec"`
}

// specRuleYAML represents a single rule in the YAML policy spec.
type specRuleYAML struct {
	Name        string   `yaml:"name"`
	Effect      string   `yaml:"effect"`
	Description string   `yaml:"description"`
	Actions     []string `yaml:"actions"`
	Resources   []string `yaml:"resources"`
	Priority    int      `yaml:"priority"`
}

// parseRulesFromSpecYAML extracts rules from a YAML policy specification.
func parseRulesFromSpecYAML(specYAML []byte) []iam.Rule {
	if len(specYAML) == 0 {
		return nil
	}

	var spec policySpecYAML
	if err := yaml.Unmarshal(specYAML, &spec); err != nil {
		return nil
	}

	if len(spec.Spec.Rules) == 0 {
		return nil
	}

	rules := make([]iam.Rule, 0, len(spec.Spec.Rules))
	for _, r := range spec.Spec.Rules {
		effect := iam.EffectDeny
		if strings.EqualFold(r.Effect, "allow") {
			effect = iam.EffectAllow
		}
		rules = append(rules, iam.Rule{
			Name:        r.Name,
			Description: r.Description,
			Effect:      effect,
			Priority:    r.Priority,
		})
	}

	return rules
}

// convertProtoPolicyToSDK converts a protobuf Policy to SDK Policy.
func convertProtoPolicyToSDK(p *iampb.Policy) *iam.Policy {
	if p == nil {
		return nil
	}

	resp := &iam.Policy{
		Name:        p.Name,
		TenantID:    p.TenantId,
		Description: p.Description,
		Priority:    int(p.Priority),
		Labels:      p.Labels,
		SpecYAML:    p.SpecYaml,
	}

	if p.CreatedAt != nil {
		resp.CreatedAt = p.CreatedAt.AsTime()
	}
	if p.UpdatedAt != nil {
		resp.UpdatedAt = p.UpdatedAt.AsTime()
	}

	// Parse rules from the YAML spec when available.
	if len(p.SpecYaml) > 0 {
		resp.Rules = parseRulesFromSpecYAML(p.SpecYaml)
	}

	return resp
}

// convertProtoAppToSDK converts a protobuf Application to SDK Application.
func convertProtoAppToSDK(a *iampb.Application) *iam.Application {
	if a == nil {
		return nil
	}

	resp := &iam.Application{
		ClientID:       a.ClientId,
		ClientSecret:   a.ClientSecret,
		TenantID:       a.TenantId,
		Name:           a.Name,
		RedirectURIs:   a.RedirectUris,
		ResourcePrefix: a.ResourcePrefix,
		ResponseTypes:  a.ResponseTypes,
	}

	// Convert grant type strings to GrantType values.
	if len(a.GrantTypes) > 0 {
		grantTypes := make([]iam.GrantType, 0, len(a.GrantTypes))
		for _, gt := range a.GrantTypes {
			grantTypes = append(grantTypes, iam.ParseGrantType(gt))
		}
		resp.GrantTypes = grantTypes
	}

	if a.CreatedAt != nil {
		resp.CreatedAt = a.CreatedAt.AsTime()
	}
	if a.UpdatedAt != nil {
		resp.UpdatedAt = a.UpdatedAt.AsTime()
	}

	return resp
}
