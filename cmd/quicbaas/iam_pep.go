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

//go:build iam

// Package main provides IAM PEP (Policy Enforcement Point) CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

var (
	// PEP command flags.
	pepSubject  string
	pepAction   string
	pepResource string
	pepContext  string
	pepFile     string
)

// newIAMPEPCommand creates the PEP command group.
func newIAMPEPCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pep",
		Short: "Policy Enforcement Point commands",
		Long: `Policy Enforcement Point (PEP) commands for authorization checks.

The PEP provides access control decision-making capabilities by evaluating
requests against configured ABAC policies.

Examples:
  # Check if a subject can perform an action
  quicbaas iam pep check --tenant acme-corp \
    --subject '{"id":"user1","roles":["admin"]}' \
    --action read \
    --resource "keys/foo"

  # Batch check multiple access requests
  quicbaas iam pep check-batch --tenant acme-corp -f requests.json`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMPEPCheckCommand())
	cmd.AddCommand(newIAMPEPCheckBatchCommand())

	return cmd
}

// newIAMPEPCheckCommand creates the PEP check command.
func newIAMPEPCheckCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check if a subject is allowed to perform an action",
		Long: `Check if a subject is allowed to perform an action on a resource.

The subject should be provided as JSON with relevant attributes such as
id, roles, groups, and custom attributes.

Examples:
  # Check if user can read a key
  quicbaas iam pep check --tenant acme-corp \
    --subject '{"id":"user1","roles":["admin"]}' \
    --action read \
    --resource "keys/foo"

  # Check with additional context
  quicbaas iam pep check --tenant acme-corp \
    --subject '{"id":"user1","roles":["developer"]}' \
    --action write \
    --resource "orders://orders/123" \
    --context '{"ip":"192.168.1.1","time":"2025-01-01T12:00:00Z"}'

  # Check with JSON output
  quicbaas iam pep check --tenant acme-corp \
    --subject '{"id":"user1","roles":["viewer"]}' \
    --action delete \
    --resource "keys/bar" \
    --output json`,
		RunE: runIAMPEPCheckCommand,
	}

	cmd.Flags().StringVar(&pepSubject, "subject", "", "Subject as JSON (required)")
	cmd.Flags().StringVar(&pepAction, "action", "", "Action to check (required)")
	cmd.Flags().StringVar(&pepResource, "resource", "", "Resource to check (required)")
	cmd.Flags().StringVar(&pepContext, "context", "", "Optional context as JSON")
	if err := cmd.MarkFlagRequired("subject"); err != nil {
		log.Warnf("Failed to mark subject flag as required: %v", err)
	}
	if err := cmd.MarkFlagRequired("action"); err != nil {
		log.Warnf("Failed to mark action flag as required: %v", err)
	}
	if err := cmd.MarkFlagRequired("resource"); err != nil {
		log.Warnf("Failed to mark resource flag as required: %v", err)
	}

	return cmd
}

// runIAMPEPCheckCommand executes the PEP check command.
func runIAMPEPCheckCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.pep.check", "--tenant flag is required")
	}

	// Parse subject JSON
	var subjectData map[string]interface{}
	if err := json.Unmarshal([]byte(pepSubject), &subjectData); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "iam.pep.check", "failed to parse subject JSON")
	}

	// Parse optional context JSON
	var requestContext map[string]interface{}
	if pepContext != "" {
		if err := json.Unmarshal([]byte(pepContext), &requestContext); err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "iam.pep.check", "failed to parse context JSON")
		}
	}

	// Build CheckSubject from parsed data
	subject := iam.CheckSubject{
		TenantID: globalTenantID,
	}

	// Extract subject ID
	if id, ok := subjectData["id"].(string); ok {
		subject.ID = id
	}

	// Extract roles
	if roles, ok := subjectData["roles"].([]interface{}); ok {
		for _, r := range roles {
			if role, ok := r.(string); ok {
				subject.Roles = append(subject.Roles, role)
			}
		}
	}

	// Extract groups
	if groups, ok := subjectData["groups"].([]interface{}); ok {
		for _, g := range groups {
			if group, ok := g.(string); ok {
				subject.Groups = append(subject.Groups, group)
			}
		}
	}

	// Set attributes from remaining subject data
	subject.Attributes = subjectData

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Perform access check using SDK sub-client
	log.Debugf("Checking access for tenant: %s", globalTenantID)
	result, err := iamClient.PEP().Check(ctx, &iam.CheckRequest{
		Subject:  subject,
		Action:   pepAction,
		Resource: pepResource,
		Context:  requestContext,
		TenantID: globalTenantID,
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
		decision := "DENY"
		if result.Allowed {
			decision = "ALLOW"
		}
		fmt.Printf("Decision: %s\n", decision)
		fmt.Printf("  Action: %s\n", pepAction)
		fmt.Printf("  Resource: %s\n", pepResource)
		if result.MatchedPolicy != "" {
			fmt.Printf("  Matched Policy: %s\n", result.MatchedPolicy)
		}
		if result.MatchedRule != "" {
			fmt.Printf("  Matched Rule: %s\n", result.MatchedRule)
		}
		if result.Reason != "" {
			fmt.Printf("  Reason: %s\n", result.Reason)
		}
		fmt.Printf("  Evaluation Time: %d ns\n", result.EvaluationTimeNs)
	}

	return nil
}

// newIAMPEPCheckBatchCommand creates the PEP batch check command.
func newIAMPEPCheckBatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-batch",
		Short: "Check multiple access requests in batch",
		Long: `Check multiple access requests in a single batch operation.

The batch file should be a JSON file containing an array of check requests.

Example batch file (requests.json):
  {
    "requests": [
      {"subject": {"id": "user1", "roles": ["admin"]}, "action": "read", "resource": "keys/foo"},
      {"subject": {"id": "user2", "roles": ["viewer"]}, "action": "write", "resource": "keys/bar"}
    ]
  }

Examples:
  # Batch check from file
  quicbaas iam pep check-batch --tenant acme-corp -f requests.json

  # Batch check with JSON output
  quicbaas iam pep check-batch --tenant acme-corp -f requests.json --output json`,
		RunE: runIAMPEPCheckBatchCommand,
	}

	cmd.Flags().StringVarP(&pepFile, "file", "f", "", "JSON file with array of check requests (required)")
	if err := cmd.MarkFlagRequired("file"); err != nil {
		log.Warnf("Failed to mark file flag as required: %v", err)
	}

	return cmd
}

// BatchCheckFile represents the structure of a batch check file.
type BatchCheckFile struct {
	Requests []BatchCheckRequest `json:"requests"`
}

// BatchCheckRequest represents a single check request in a batch file.
type BatchCheckRequest struct {
	Subject  map[string]interface{} `json:"subject"`
	Action   string                 `json:"action"`
	Resource string                 `json:"resource"`
	Context  map[string]interface{} `json:"context,omitempty"`
}

// runIAMPEPCheckBatchCommand executes the PEP batch check command.
func runIAMPEPCheckBatchCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.pep.check-batch", "--tenant flag is required")
	}

	// Read batch file
	data, err := os.ReadFile(pepFile)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "iam.pep.check-batch", "failed to read batch file")
	}

	// Parse batch file
	var batchFile BatchCheckFile
	if err := json.Unmarshal(data, &batchFile); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "iam.pep.check-batch", "failed to parse batch file JSON")
	}

	if len(batchFile.Requests) == 0 {
		return errors.Newf(errors.ErrInvalidInput, "iam.pep.check-batch", "batch file contains no requests")
	}

	// Convert to SDK CheckRequests
	requests := make([]iam.CheckRequest, len(batchFile.Requests))
	for i, req := range batchFile.Requests {
		subject := iam.CheckSubject{
			TenantID:   globalTenantID,
			Attributes: req.Subject,
		}

		// Extract subject ID
		if id, ok := req.Subject["id"].(string); ok {
			subject.ID = id
		}

		// Extract roles
		if roles, ok := req.Subject["roles"].([]interface{}); ok {
			for _, r := range roles {
				if role, ok := r.(string); ok {
					subject.Roles = append(subject.Roles, role)
				}
			}
		}

		// Extract groups
		if groups, ok := req.Subject["groups"].([]interface{}); ok {
			for _, g := range groups {
				if group, ok := g.(string); ok {
					subject.Groups = append(subject.Groups, group)
				}
			}
		}

		requests[i] = iam.CheckRequest{
			Subject:  subject,
			Action:   req.Action,
			Resource: req.Resource,
			Context:  req.Context,
			TenantID: globalTenantID,
		}
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Perform batch access check using SDK sub-client
	log.Debugf("Performing batch access check for tenant: %s (%d requests)", globalTenantID, len(requests))
	result, err := iamClient.PEP().CheckBatch(ctx, &iam.CheckBatchRequest{
		Requests: requests,
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
		return encoder.Encode(map[string]interface{}{
			"results":     result.Results,
			"all_allowed": result.AllAllowed,
		})

	default:
		// Table output
		fmt.Printf("%-8s %-15s %-30s %-20s %-20s\n", "RESULT", "ACTION", "RESOURCE", "MATCHED POLICY", "MATCHED RULE")
		fmt.Println(strings.Repeat("-", 100))

		for i, res := range result.Results {
			decision := "DENY"
			if res.Allowed {
				decision = "ALLOW"
			}

			// Truncate long values for table display
			action := batchFile.Requests[i].Action
			resource := batchFile.Requests[i].Resource
			if len(resource) > 28 {
				resource = resource[:25] + "..."
			}

			matchedPolicy := res.MatchedPolicy
			if len(matchedPolicy) > 18 {
				matchedPolicy = matchedPolicy[:15] + "..."
			}

			matchedRule := res.MatchedRule
			if len(matchedRule) > 18 {
				matchedRule = matchedRule[:15] + "..."
			}

			fmt.Printf("%-8s %-15s %-30s %-20s %-20s\n",
				decision,
				action,
				resource,
				matchedPolicy,
				matchedRule,
			)
		}

		fmt.Println(strings.Repeat("-", 100))
		fmt.Printf("Total: %d requests | Allowed: %d | Denied: %d\n", len(result.Results), result.AllowedCount, result.DeniedCount)
	}

	return nil
}
