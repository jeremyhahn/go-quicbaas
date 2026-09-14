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

// Package main provides IAM policy management CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

var (
	// Policy command flags.
	iamPolicyFile     string
	iamPolicySubject  string
	iamPolicyAction   string
	iamPolicyResource string
)

// newIAMPolicyCommand creates the policy command group.
func newIAMPolicyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "ABAC policy management commands",
		Long: `Manage attribute-based access control (ABAC) policies.

Policies define fine-grained access control rules based on subject
attributes (user roles, groups), actions, resources, and context.

Policies are written in YAML format and can be tested before applying.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMPolicyApplyCommand())
	cmd.AddCommand(newIAMPolicyGetCommand())
	cmd.AddCommand(newIAMPolicyDeleteCommand())
	cmd.AddCommand(newIAMPolicyListCommand())
	cmd.AddCommand(newIAMPolicyValidateCommand())
	cmd.AddCommand(newIAMPolicyTestCommand())

	return cmd
}

// newIAMPolicyApplyCommand creates the policy apply command.
func newIAMPolicyApplyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a policy from file",
		Long: `Apply an ABAC policy from a YAML file.

The policy file must conform to the QuicBaaS policy schema.

Example policy file (policy.yaml):
  apiVersion: abac.quicbaas.io/v1
  kind: Policy
  metadata:
    name: order-access
    tenant: acme-corp
  spec:
    rules:
      - name: read-own
        effect: allow
        actions: [read]
        resources:
          - orders://orders/${{subject.id}}/**
        subjects:
          authenticated: true

Examples:
  # Apply policy from file
  quicbaas iam policy apply -f policy.yaml

  # Apply policy with output
  quicbaas iam policy apply -f policy.yaml --output json`,
		RunE: runIAMPolicyApplyCommand,
	}

	cmd.Flags().StringVarP(&iamPolicyFile, "file", "f", "", "Policy file path (required)")
	cmd.MarkFlagRequired("file")

	return cmd
}

// runIAMPolicyApplyCommand executes the policy apply command.
func runIAMPolicyApplyCommand(cmd *cobra.Command, args []string) error {
	// Read policy file
	data, err := os.ReadFile(iamPolicyFile)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "iam.policy.apply", "failed to read policy file")
	}

	// Parse policy YAML
	var policyData map[string]interface{}
	if err := yaml.Unmarshal(data, &policyData); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "iam.policy.apply", "failed to parse policy YAML")
	}

	// Extract metadata
	metadata, ok := policyData["metadata"].(map[string]interface{})
	if !ok {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.apply", "policy must have metadata section")
	}

	policyName, ok := metadata["name"].(string)
	if !ok || policyName == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.apply", "policy must have metadata.name")
	}

	tenantID, ok := metadata["tenant"].(string)
	if !ok || tenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.apply", "policy must have metadata.tenant")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Build the policy request from YAML data, include raw YAML for server-side parsing
	policyReq := buildPolicyRequest(policyData, policyName, data)

	// Apply policy using SDK sub-client
	log.Infof("Applying policy: %s in tenant: %s", policyName, tenantID)
	appliedPolicy, err := iamClient.Policy().Create(ctx, tenantID, policyReq)
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(appliedPolicy)

	default:
		fmt.Println("Policy applied successfully:")
		fmt.Printf("  Name: %s\n", appliedPolicy.Name)
		fmt.Printf("  Tenant: %s\n", appliedPolicy.TenantID)
		fmt.Printf("  Rules: %d\n", len(appliedPolicy.Rules))
		fmt.Printf("  Created: %s\n", appliedPolicy.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// buildPolicyRequest builds a CreatePolicyRequest from YAML data.
func buildPolicyRequest(policyData map[string]interface{}, name string, rawYAML []byte) *iam.CreatePolicyRequest {
	req := &iam.CreatePolicyRequest{
		Name:     name,
		Enabled:  true,
		SpecYAML: rawYAML, // Pass raw YAML for server-side parsing
	}

	// Extract description if present
	if metadata, ok := policyData["metadata"].(map[string]interface{}); ok {
		if desc, ok := metadata["description"].(string); ok {
			req.Description = desc
		}
	}

	// Extract spec if present
	if spec, ok := policyData["spec"].(map[string]interface{}); ok {
		// Extract rules
		if rulesRaw, ok := spec["rules"].([]interface{}); ok {
			for _, ruleRaw := range rulesRaw {
				if ruleMap, ok := ruleRaw.(map[string]interface{}); ok {
					rule := iam.Rule{}
					if name, ok := ruleMap["name"].(string); ok {
						rule.Name = name
					}
					if effect, ok := ruleMap["effect"].(string); ok {
						if effect == "allow" {
							rule.Effect = iam.EffectAllow
						} else {
							rule.Effect = iam.EffectDeny
						}
					}
					req.Rules = append(req.Rules, rule)
				}
			}
		}
	}

	return req
}

// newIAMPolicyGetCommand creates the policy get command.
func newIAMPolicyGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get policy details",
		Long: `Retrieve a specific policy by name.

Examples:
  # Get policy details
  quicbaas iam policy get order-access --tenant acme-corp

  # Get policy as YAML
  quicbaas iam policy get order-access --tenant acme-corp --output yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMPolicyGetCommand,
	}

	return cmd
}

// runIAMPolicyGetCommand executes the policy get command.
func runIAMPolicyGetCommand(cmd *cobra.Command, args []string) error {
	policyName := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.get", "--tenant flag is required")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Get policy using SDK sub-client
	log.Infof("Getting policy: %s in tenant: %s", policyName, globalTenantID)
	policy, err := iamClient.Policy().Get(ctx, globalTenantID, policyName)
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(policy)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(os.Stdout)
		encoder.SetIndent(2)
		defer encoder.Close()
		return encoder.Encode(policy)

	default:
		fmt.Println("Policy Details:")
		fmt.Printf("  Name: %s\n", policy.Name)
		fmt.Printf("  Tenant: %s\n", policy.TenantID)
		fmt.Printf("  Rules: %d\n", len(policy.Rules))
		for _, rule := range policy.Rules {
			effect := "allow"
			if rule.Effect == iam.EffectDeny {
				effect = "deny"
			}
			fmt.Printf("    - %s (%s)\n", rule.Name, effect)
		}
		fmt.Printf("  Created: %s\n", policy.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Updated: %s\n", policy.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMPolicyDeleteCommand creates the policy delete command.
func newIAMPolicyDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a policy",
		Long: `Delete a policy from the tenant.

WARNING: This operation is irreversible and may affect access control
for users and applications relying on this policy.

Examples:
  # Delete policy with confirmation
  quicbaas iam policy delete order-access --tenant acme-corp

  # Delete policy without confirmation
  quicbaas iam policy delete order-access --tenant acme-corp --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMPolicyDeleteCommand,
	}

	return cmd
}

// runIAMPolicyDeleteCommand executes the policy delete command.
func runIAMPolicyDeleteCommand(cmd *cobra.Command, args []string) error {
	policyName := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.delete", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to delete policy '%s'? This cannot be undone. (y/N): ", policyName)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Delete cancelled")
			return nil
		}
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Delete policy using SDK sub-client
	log.Infof("Deleting policy: %s from tenant: %s", policyName, globalTenantID)
	if err := iamClient.Policy().Delete(ctx, globalTenantID, policyName); err != nil {
		return formatError(err)
	}

	fmt.Printf("Policy '%s' deleted successfully\n", policyName)
	return nil
}

// newIAMPolicyListCommand creates the policy list command.
func newIAMPolicyListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List policies in a tenant",
		Long: `List all policies within a tenant.

Examples:
  # List all policies
  quicbaas iam policy list --tenant acme-corp

  # List policies as JSON
  quicbaas iam policy list --tenant acme-corp --output json`,
		RunE: runIAMPolicyListCommand,
	}

	return cmd
}

// runIAMPolicyListCommand executes the policy list command.
func runIAMPolicyListCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.list", "--tenant flag is required")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// List policies using SDK sub-client
	log.Infof("Listing policies in tenant: %s", globalTenantID)
	resp, err := iamClient.Policy().List(ctx, globalTenantID, &iam.ListPoliciesRequest{})
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp.Policies)

	default:
		if len(resp.Policies) == 0 {
			fmt.Println("No policies found")
			return nil
		}

		fmt.Printf("%-30s %-10s %s\n", "NAME", "RULES", "CREATED")
		fmt.Println(strings.Repeat("-", 70))
		for _, policy := range resp.Policies {
			fmt.Printf("%-30s %-10d %s\n",
				policy.Name,
				len(policy.Rules),
				policy.CreatedAt.Format("2006-01-02 15:04"))
		}
		fmt.Printf("\nTotal: %d policies\n", len(resp.Policies))
	}

	return nil
}

// newIAMPolicyValidateCommand creates the policy validate command.
func newIAMPolicyValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a policy file (dry run)",
		Long: `Validate a policy file without applying it.

This performs syntax validation and checks for common errors
without creating or modifying the policy on the server.

Examples:
  # Validate policy file
  quicbaas iam policy validate -f policy.yaml`,
		RunE: runIAMPolicyValidateCommand,
	}

	cmd.Flags().StringVarP(&iamPolicyFile, "file", "f", "", "Policy file path (required)")
	cmd.MarkFlagRequired("file")

	return cmd
}

// runIAMPolicyValidateCommand executes the policy validate command.
func runIAMPolicyValidateCommand(cmd *cobra.Command, args []string) error {
	// Read policy file
	data, err := os.ReadFile(iamPolicyFile)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "iam.policy.validate", "failed to read policy file")
	}

	// Parse policy YAML
	var policyData map[string]interface{}
	if err := yaml.Unmarshal(data, &policyData); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "iam.policy.validate", "failed to parse policy YAML")
	}

	// Extract metadata
	metadata, ok := policyData["metadata"].(map[string]interface{})
	if !ok {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.validate", "policy must have metadata section")
	}

	policyName, ok := metadata["name"].(string)
	if !ok || policyName == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.validate", "policy must have metadata.name")
	}

	tenantID, ok := metadata["tenant"].(string)
	if !ok || tenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.validate", "policy must have metadata.tenant")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Build policy for validation with raw YAML spec
	policy := &iam.Policy{
		Name:     policyName,
		TenantID: tenantID,
		SpecYAML: data, // Pass full YAML spec for server-side validation
	}

	// Validate policy using SDK sub-client
	log.Infof("Validating policy: %s", policyName)
	result, err := iamClient.Policy().Validate(ctx, tenantID, policy)
	if err != nil {
		return formatError(err)
	}

	// Format output
	if result.Valid {
		fmt.Println("Policy is valid")
		fmt.Printf("  Name: %s\n", policyName)
		fmt.Printf("  Tenant: %s\n", tenantID)
	} else {
		fmt.Println("Policy validation failed:")
		for i, issue := range result.Errors {
			fmt.Printf("  %d. %s: %s\n", i+1, issue.Field, issue.Message)
		}
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.validate", "policy validation failed")
	}

	return nil
}

// newIAMPolicyTestCommand creates the policy test command.
func newIAMPolicyTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test policy decision",
		Long: `Test an authorization decision using the policy engine.

This allows you to test whether a specific subject would be allowed
to perform an action on a resource given the current policies.

The subject should be provided as JSON with relevant attributes.

Examples:
  # Test policy decision
  quicbaas iam policy test --tenant acme-corp \
    --subject '{"id":"user1","roles":["admin"]}' \
    --action read \
    --resource "keys/foo"

  # Test with output
  quicbaas iam policy test --tenant acme-corp \
    --subject '{"id":"user1","roles":["developer"]}' \
    --action write \
    --resource "orders://orders/123" \
    --output json`,
		RunE: runIAMPolicyTestCommand,
	}

	cmd.Flags().StringVar(&iamPolicySubject, "subject", "", "Subject as JSON (required)")
	cmd.Flags().StringVar(&iamPolicyAction, "action", "", "Action to test (required)")
	cmd.Flags().StringVar(&iamPolicyResource, "resource", "", "Resource to test (required)")
	cmd.MarkFlagRequired("subject")
	cmd.MarkFlagRequired("action")
	cmd.MarkFlagRequired("resource")

	return cmd
}

// runIAMPolicyTestCommand executes the policy test command.
func runIAMPolicyTestCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.policy.test", "--tenant flag is required")
	}

	// Parse subject JSON
	var subjectMap map[string]interface{}
	if err := json.Unmarshal([]byte(iamPolicySubject), &subjectMap); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "iam.policy.test", "failed to parse subject JSON")
	}

	// Build subject from map
	subject := iam.Subject{}
	if id, ok := subjectMap["id"].(string); ok {
		subject.ID = id
	}
	if rolesRaw, ok := subjectMap["roles"].([]interface{}); ok {
		for _, r := range rolesRaw {
			if role, ok := r.(string); ok {
				subject.Roles = append(subject.Roles, role)
			}
		}
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Simulate policy using SDK sub-client
	log.Infof("Testing policy decision for tenant: %s", globalTenantID)
	result, err := iamClient.Policy().Simulate(ctx, globalTenantID, &iam.SimulateRequest{
		Subject:  subject,
		Action:   iamPolicyAction,
		Resource: iamPolicyResource,
	})
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)

	default:
		fmt.Println("Policy Test Result:")
		fmt.Printf("  Decision: %s\n", map[bool]string{true: "ALLOW", false: "DENY"}[result.Allowed])
		fmt.Printf("  Action: %s\n", iamPolicyAction)
		fmt.Printf("  Resource: %s\n", iamPolicyResource)
		if len(result.MatchedPolicies) > 0 {
			fmt.Printf("  Matched Policies: %s\n", strings.Join(result.MatchedPolicies, ", "))
		}
		if len(result.MatchedRules) > 0 {
			fmt.Printf("  Matched Rules: %s\n", strings.Join(result.MatchedRules, ", "))
		}
		fmt.Printf("  Evaluation Time: %d ns\n", result.EvaluationTimeNs)
	}

	return nil
}
