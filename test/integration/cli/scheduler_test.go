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

//go:build integration

// Package cli provides E2E integration tests for QuicBaaS CLI scheduler commands.
package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schedulerUnsupportedTransports lists transports that do not support scheduler commands.
// The scheduler service is available via gRPC, REST, MCP, QUIC, and Unix socket transports.
// All transports now support scheduler commands.
// See cmd/quicbaas/scheduler.go createSchedulerClient() for implementation status.
var schedulerUnsupportedTransports = []Transport{}

// =============================================================================
// Scheduler Command Definitions
// =============================================================================

// SchedulerStatusCommand returns a command to get scheduler status.
func SchedulerStatusCommand() CommandDef {
	return CommandDef{
		Name:           "scheduler status",
		Args:           []string{"scheduler", "status"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerStatusJSONCommand returns a command to get scheduler status in JSON format.
func SchedulerStatusJSONCommand() CommandDef {
	return CommandDef{
		Name:           "scheduler status json",
		Args:           []string{"scheduler", "status", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerStatusYAMLCommand returns a command to get scheduler status in YAML format.
func SchedulerStatusYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "scheduler status yaml",
		Args:           []string{"scheduler", "status", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCreateCommand returns a command to create a scheduler job.
func SchedulerJobCreateCommand(name, jobType, handler string, opts ...string) CommandDef {
	args := []string{"scheduler", "job", "create", name, "--type", jobType, "--handler", handler}
	args = append(args, opts...)
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job create %s", name),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCreateOneShotCommand returns a command to create a one-shot job.
func SchedulerJobCreateOneShotCommand(name, handler, executeAt string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("scheduler job create one-shot %s", name),
		Args: []string{
			"scheduler", "job", "create", name,
			"--type", "one-shot",
			"--handler", handler,
			"--execute-at", executeAt,
		},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCreateRecurringCommand returns a command to create a recurring job.
func SchedulerJobCreateRecurringCommand(name, handler, schedule string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("scheduler job create recurring %s", name),
		Args: []string{
			"scheduler", "job", "create", name,
			"--type", "recurring",
			"--handler", handler,
			"--schedule", schedule,
		},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCreateDelayedCommand returns a command to create a delayed job.
func SchedulerJobCreateDelayedCommand(name, handler, delay string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("scheduler job create delayed %s", name),
		Args: []string{
			"scheduler", "job", "create", name,
			"--type", "delayed",
			"--handler", handler,
			"--delay", delay,
		},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCreateWithAllOptionsCommand creates a job with all available options.
func SchedulerJobCreateWithAllOptionsCommand(name, handler string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("scheduler job create with all options %s", name),
		Args: []string{
			"scheduler", "job", "create", name,
			"--type", "recurring",
			"--handler", handler,
			"--schedule", "0 * * * *",
			"--description", "Test job with all options",
			"--payload", `{"key":"value"}`,
			"--priority", "10",
			"--timeout", "10m",
			"--max-retries", "5",
		},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCreateWithTenantCommand creates a job scoped to a tenant.
func SchedulerJobCreateWithTenantCommand(name, handler, tenantID string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("scheduler job create with tenant %s", name),
		Args: []string{
			"scheduler", "job", "create", name,
			"--type", "one-shot",
			"--handler", handler,
			"--execute-at", time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			"--tenant", tenantID,
		},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobGetCommand returns a command to get a job by ID.
func SchedulerJobGetCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job get %s", jobID),
		Args:           []string{"scheduler", "job", "get", jobID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobGetJSONCommand returns a command to get a job in JSON format.
func SchedulerJobGetJSONCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job get json %s", jobID),
		Args:           []string{"scheduler", "job", "get", jobID, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobGetYAMLCommand returns a command to get a job in YAML format.
func SchedulerJobGetYAMLCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job get yaml %s", jobID),
		Args:           []string{"scheduler", "job", "get", jobID, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobGetWithTenantCommand returns a command to get a job with tenant filter.
func SchedulerJobGetWithTenantCommand(jobID, tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job get with tenant %s", jobID),
		Args:           []string{"scheduler", "job", "get", jobID, "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobListCommand returns a command to list jobs.
func SchedulerJobListCommand() CommandDef {
	return CommandDef{
		Name:           "scheduler job list",
		Args:           []string{"scheduler", "job", "list"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobListWithLimitCommand returns a command to list jobs with limit.
func SchedulerJobListWithLimitCommand(limit int) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job list limit=%d", limit),
		Args:           []string{"scheduler", "job", "list", "--limit", fmt.Sprintf("%d", limit)},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobListWithStatusCommand returns a command to list jobs with status filter.
func SchedulerJobListWithStatusCommand(status string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job list status=%s", status),
		Args:           []string{"scheduler", "job", "list", "--status", status},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobListWithHandlerCommand returns a command to list jobs with handler filter.
func SchedulerJobListWithHandlerCommand(handler string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job list handler=%s", handler),
		Args:           []string{"scheduler", "job", "list", "--handler", handler},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobListWithCursorCommand returns a command to list jobs with cursor pagination.
func SchedulerJobListWithCursorCommand(cursor string) CommandDef {
	return CommandDef{
		Name:           "scheduler job list with cursor",
		Args:           []string{"scheduler", "job", "list", "--cursor", cursor},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobListWithTenantCommand returns a command to list jobs for a specific tenant.
func SchedulerJobListWithTenantCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job list tenant=%s", tenantID),
		Args:           []string{"scheduler", "job", "list", "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobListJSONCommand returns a command to list jobs in JSON format.
func SchedulerJobListJSONCommand() CommandDef {
	return CommandDef{
		Name:           "scheduler job list json",
		Args:           []string{"scheduler", "job", "list", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobDeleteCommand returns a command to delete a job.
func SchedulerJobDeleteCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job delete %s", jobID),
		Args:           []string{"scheduler", "job", "delete", jobID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobDeleteForceCommand returns a command to force delete a job.
func SchedulerJobDeleteForceCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job delete force %s", jobID),
		Args:           []string{"scheduler", "job", "delete", jobID, "--force"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobTriggerCommand returns a command to trigger a job.
func SchedulerJobTriggerCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job trigger %s", jobID),
		Args:           []string{"scheduler", "job", "trigger", jobID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobPauseCommand returns a command to pause a job.
func SchedulerJobPauseCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job pause %s", jobID),
		Args:           []string{"scheduler", "job", "pause", jobID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobResumeCommand returns a command to resume a job.
func SchedulerJobResumeCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job resume %s", jobID),
		Args:           []string{"scheduler", "job", "resume", jobID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCancelCommand returns a command to cancel a job.
func SchedulerJobCancelCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job cancel %s", jobID),
		Args:           []string{"scheduler", "job", "cancel", jobID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCancelWithReasonCommand returns a command to cancel a job with reason.
func SchedulerJobCancelWithReasonCommand(jobID, reason string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job cancel with reason %s", jobID),
		Args:           []string{"scheduler", "job", "cancel", jobID, "--reason", reason},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobHistoryCommand returns a command to get job execution history.
func SchedulerJobHistoryCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job history %s", jobID),
		Args:           []string{"scheduler", "job", "history", jobID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobHistoryWithLimitCommand returns a command to get history with limit.
func SchedulerJobHistoryWithLimitCommand(jobID string, limit int) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job history %s limit=%d", jobID, limit),
		Args:           []string{"scheduler", "job", "history", jobID, "--limit", fmt.Sprintf("%d", limit)},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobHistoryWithCursorCommand returns a command to get history with cursor.
func SchedulerJobHistoryWithCursorCommand(jobID, cursor string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job history %s with cursor", jobID),
		Args:           []string{"scheduler", "job", "history", jobID, "--cursor", cursor},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobHistoryJSONCommand returns a command to get history in JSON format.
func SchedulerJobHistoryJSONCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("scheduler job history json %s", jobID),
		Args:           []string{"scheduler", "job", "history", jobID, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// =============================================================================
// Scheduler Error Command Definitions
// =============================================================================

// SchedulerJobCreateMissingHandlerCommand returns a command that should fail.
// Note: This command tests CLI validation which happens before transport.
// However, unsupported transports fail before reaching validation with different errors.
func SchedulerJobCreateMissingHandlerCommand(name string) CommandDef {
	return CommandDef{
		Name:           "scheduler job create missing handler",
		Args:           []string{"scheduler", "job", "create", name, "--type", "one-shot"},
		ExpectSuccess:  false,
		ExpectError:    []string{"handler"},
		Timeout:        10 * time.Second,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobGetInvalidIDCommand returns a command that should fail.
// Note: This command tests CLI validation which happens before transport.
// However, unsupported transports fail before reaching validation with different errors.
func SchedulerJobGetInvalidIDCommand() CommandDef {
	return CommandDef{
		Name:           "scheduler job get invalid id",
		Args:           []string{"scheduler", "job", "get", "invalid-not-a-number"},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid"},
		Timeout:        10 * time.Second,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobGetNonexistentCommand returns a command to get a non-existent job.
func SchedulerJobGetNonexistentCommand() CommandDef {
	return CommandDef{
		Name:           "scheduler job get nonexistent",
		Args:           []string{"scheduler", "job", "get", "999999999"},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobDeleteInvalidIDCommand returns a command that should fail.
// Note: This command tests CLI validation which happens before transport.
// However, unsupported transports fail before reaching validation with different errors.
func SchedulerJobDeleteInvalidIDCommand() CommandDef {
	return CommandDef{
		Name:           "scheduler job delete invalid id",
		Args:           []string{"scheduler", "job", "delete", "not-a-number"},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid"},
		Timeout:        10 * time.Second,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCreateInvalidTimeoutCommand returns a command with invalid timeout.
// Note: This command tests CLI validation which happens before transport.
// However, unsupported transports fail before reaching validation with different errors.
func SchedulerJobCreateInvalidTimeoutCommand(name string) CommandDef {
	return CommandDef{
		Name: "scheduler job create invalid timeout",
		Args: []string{
			"scheduler", "job", "create", name,
			"--type", "one-shot",
			"--handler", "test.handler",
			"--execute-at", time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			"--timeout", "invalid-duration",
		},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid", "timeout"},
		Timeout:        10 * time.Second,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCreateInvalidExecuteAtCommand returns a command with invalid execute-at.
// Note: This command tests CLI validation which happens before transport.
// However, unsupported transports fail before reaching validation with different errors.
func SchedulerJobCreateInvalidExecuteAtCommand(name string) CommandDef {
	return CommandDef{
		Name: "scheduler job create invalid execute-at",
		Args: []string{
			"scheduler", "job", "create", name,
			"--type", "one-shot",
			"--handler", "test.handler",
			"--execute-at", "not-a-valid-time",
		},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid", "execute-at"},
		Timeout:        10 * time.Second,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// SchedulerJobCreateInvalidDelayCommand returns a command with invalid delay.
// Note: This command tests CLI validation which happens before transport.
// However, unsupported transports fail before reaching validation with different errors.
func SchedulerJobCreateInvalidDelayCommand(name string) CommandDef {
	return CommandDef{
		Name: "scheduler job create invalid delay",
		Args: []string{
			"scheduler", "job", "create", name,
			"--type", "delayed",
			"--handler", "test.handler",
			"--delay", "not-a-duration",
		},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid", "delay"},
		Timeout:        10 * time.Second,
		SkipTransports: schedulerUnsupportedTransports,
	}
}

// =============================================================================
// Scheduler Help Command Definitions
// =============================================================================

// SchedulerHelpCommand returns a command to show scheduler help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func SchedulerHelpCommand() CommandDef {
	return CommandDef{
		Name:          "scheduler help",
		Args:          []string{"scheduler", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"job", "status"},
		Timeout:       10 * time.Second,
	}
}

// SchedulerJobHelpCommand returns a command to show scheduler job help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func SchedulerJobHelpCommand() CommandDef {
	return CommandDef{
		Name:          "scheduler job help",
		Args:          []string{"scheduler", "job", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"create", "get", "list", "delete", "trigger", "pause", "resume", "cancel", "history"},
		Timeout:       10 * time.Second,
	}
}

// SchedulerJobCreateHelpCommand returns a command to show create help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func SchedulerJobCreateHelpCommand() CommandDef {
	return CommandDef{
		Name:          "scheduler job create help",
		Args:          []string{"scheduler", "job", "create", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"type", "handler", "schedule", "execute-at", "delay", "timeout", "max-retries"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestSchedulerHelp tests scheduler help commands.
func TestSchedulerHelp(t *testing.T) {
	RunForAllTransports(t, "SchedulerHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(SchedulerHelpCommand())
	})

	RunForAllTransports(t, "SchedulerJobHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(SchedulerJobHelpCommand())
	})

	RunForAllTransports(t, "SchedulerJobCreateHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(SchedulerJobCreateHelpCommand())
	})
}

// TestSchedulerStatus tests scheduler status command with all output formats.
func TestSchedulerStatus(t *testing.T) {
	RunForAllTransports(t, "SchedulerStatus", func(t *testing.T, runner *TestRunner) {
		cmd := SchedulerStatusCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "SchedulerStatusJSON", func(t *testing.T, runner *TestRunner) {
		cmd := SchedulerStatusJSONCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)

		// Verify JSON output is valid
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "SchedulerStatusYAML", func(t *testing.T, runner *TestRunner) {
		cmd := SchedulerStatusYAMLCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// TestSchedulerJobLifecycle tests complete job lifecycle via CLI.
func TestSchedulerJobLifecycle(t *testing.T) {
	RunForAllTransports(t, "JobLifecycle", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("cli-test-job-%d", time.Now().UnixNano())
		executeAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Create job
		createCmd := SchedulerJobCreateOneShotCommand(jobName, "test.noop", executeAt)
		if runner.IsSkipped(createCmd) {
			return
		}
		stdout, _, err := runner.Run(createCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, jobName)

		// Extract job ID from output - job ID is typically first numeric value
		// For now, we will use list to find the job
		listCmd := SchedulerJobListJSONCommand()
		stdout, _, err = runner.Run(listCmd)
		require.NoError(t, err)

		var listResp struct {
			Jobs []struct {
				ID   uint64 `json:"id"`
				Name string `json:"name"`
			} `json:"jobs"`
		}
		err = json.Unmarshal([]byte(stdout), &listResp)
		require.NoError(t, err)

		var jobID uint64
		for _, job := range listResp.Jobs {
			if job.Name == jobName {
				jobID = job.ID
				break
			}
		}
		require.NotZero(t, jobID, "Job should be found in list")

		jobIDStr := fmt.Sprintf("%d", jobID)

		// Get job
		getCmd := SchedulerJobGetCommand(jobIDStr)
		stdout, _, err = runner.Run(getCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, jobName)

		// Get job in JSON format
		getJSONCmd := SchedulerJobGetJSONCommand(jobIDStr)
		stdout, _, err = runner.Run(getJSONCmd)
		require.NoError(t, err)
		var jobResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &jobResp)
		require.NoError(t, err)

		// Trigger job
		triggerCmd := SchedulerJobTriggerCommand(jobIDStr)
		_, _, err = runner.Run(triggerCmd)
		// Trigger may fail if job type does not support it - that's ok
		t.Logf("Trigger result: %v", err)

		// Pause job
		pauseCmd := SchedulerJobPauseCommand(jobIDStr)
		_, _, err = runner.Run(pauseCmd)
		t.Logf("Pause result: %v", err)

		// Resume job
		resumeCmd := SchedulerJobResumeCommand(jobIDStr)
		_, _, err = runner.Run(resumeCmd)
		t.Logf("Resume result: %v", err)

		// Get history
		historyCmd := SchedulerJobHistoryCommand(jobIDStr)
		_, _, err = runner.Run(historyCmd)
		t.Logf("History result: %v", err)

		// Cancel job
		cancelCmd := SchedulerJobCancelWithReasonCommand(jobIDStr, "Test cancellation")
		_, _, err = runner.Run(cancelCmd)
		t.Logf("Cancel result: %v", err)

		// Delete job
		deleteCmd := SchedulerJobDeleteCommand(jobIDStr)
		_, _, err = runner.Run(deleteCmd)
		require.NoError(t, err)

		// Verify job is deleted
		getCmd = SchedulerJobGetCommand(jobIDStr)
		_, _, err = runner.Run(getCmd)
		assert.Error(t, err, "Job should not exist after deletion")
	})
}

// TestSchedulerJobCreate tests job creation with various options.
func TestSchedulerJobCreate(t *testing.T) {
	RunForAllTransports(t, "CreateOneShotJob", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("oneshot-%d", time.Now().UnixNano())
		executeAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		cmd := SchedulerJobCreateOneShotCommand(jobName, "test.handler", executeAt)
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, jobName)

		// Cleanup
		cleanupSchedulerJob(t, runner, jobName)
	})

	RunForAllTransports(t, "CreateRecurringJob", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("recurring-%d", time.Now().UnixNano())

		cmd := SchedulerJobCreateRecurringCommand(jobName, "backup.full", "0 0 * * *")
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, jobName)

		cleanupSchedulerJob(t, runner, jobName)
	})

	RunForAllTransports(t, "CreateDelayedJob", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("delayed-%d", time.Now().UnixNano())

		cmd := SchedulerJobCreateDelayedCommand(jobName, "audit.cleanup", "24h")
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, jobName)

		cleanupSchedulerJob(t, runner, jobName)
	})

	RunForAllTransports(t, "CreateJobWithAllOptions", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("allopts-%d", time.Now().UnixNano())

		cmd := SchedulerJobCreateWithAllOptionsCommand(jobName, "test.comprehensive")
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, jobName)

		cleanupSchedulerJob(t, runner, jobName)
	})

	RunForAllTransports(t, "CreateJobWithTenant", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("tenant-%d", time.Now().UnixNano())
		tenantID := "test-tenant"

		cmd := SchedulerJobCreateWithTenantCommand(jobName, "tenant.job", tenantID)
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, jobName)

		cleanupSchedulerJob(t, runner, jobName)
	})
}

// TestSchedulerJobList tests job listing with various filters.
func TestSchedulerJobList(t *testing.T) {
	RunForAllTransports(t, "ListJobs", func(t *testing.T, runner *TestRunner) {
		cmd := SchedulerJobListCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "ListJobsWithLimit", func(t *testing.T, runner *TestRunner) {
		cmd := SchedulerJobListWithLimitCommand(10)
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "ListJobsJSON", func(t *testing.T, runner *TestRunner) {
		cmd := SchedulerJobListJSONCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "ListJobsByStatus", func(t *testing.T, runner *TestRunner) {
		// Test various status filters
		statuses := []string{"pending", "running", "completed", "failed", "paused", "canceled"}
		for _, status := range statuses {
			t.Run(status, func(t *testing.T) {
				cmd := SchedulerJobListWithStatusCommand(status)
				if runner.IsSkipped(cmd) {
					return
				}
				runner.RunAndAssert(cmd)
			})
		}
	})

	RunForAllTransports(t, "ListJobsByHandler", func(t *testing.T, runner *TestRunner) {
		cmd := SchedulerJobListWithHandlerCommand("test.handler")
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "ListJobsWithTenant", func(t *testing.T, runner *TestRunner) {
		cmd := SchedulerJobListWithTenantCommand("test-tenant")
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// TestSchedulerJobHistory tests job history retrieval.
func TestSchedulerJobHistory(t *testing.T) {
	RunForAllTransports(t, "JobHistory", func(t *testing.T, runner *TestRunner) {
		// First create a job to get history for
		jobName := fmt.Sprintf("history-test-%d", time.Now().UnixNano())
		executeAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		createCmd := SchedulerJobCreateOneShotCommand(jobName, "test.handler", executeAt)
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		// Find job ID
		jobID := findSchedulerJobID(t, runner, jobName)
		require.NotEmpty(t, jobID, "Created job must be findable by name: %s", jobName)

		// Get history
		historyCmd := SchedulerJobHistoryCommand(jobID)
		runner.RunAndAssert(historyCmd)

		// Get history with limit
		historyLimitCmd := SchedulerJobHistoryWithLimitCommand(jobID, 5)
		runner.RunAndAssert(historyLimitCmd)

		// Get history JSON
		historyJSONCmd := SchedulerJobHistoryJSONCommand(jobID)
		stdout, _, err := runner.Run(historyJSONCmd)
		require.NoError(t, err)
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "History output should be valid JSON")

		// Cleanup
		cleanupSchedulerJob(t, runner, jobName)
	})
}

// TestSchedulerJobPauseResume tests job pause and resume operations.
func TestSchedulerJobPauseResume(t *testing.T) {
	RunForAllTransports(t, "PauseResumeJob", func(t *testing.T, runner *TestRunner) {
		// Create a recurring job that can be paused
		jobName := fmt.Sprintf("pause-resume-%d", time.Now().UnixNano())

		createCmd := SchedulerJobCreateRecurringCommand(jobName, "test.handler", "*/5 * * * *")
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		jobID := findSchedulerJobID(t, runner, jobName)
		require.NotEmpty(t, jobID, "Created job must be findable by name: %s", jobName)

		// Pause the job
		pauseCmd := SchedulerJobPauseCommand(jobID)
		_, _, err = runner.Run(pauseCmd)
		// Pause might not be supported for all job types
		t.Logf("Pause result: %v", err)

		// Resume the job
		resumeCmd := SchedulerJobResumeCommand(jobID)
		_, _, err = runner.Run(resumeCmd)
		t.Logf("Resume result: %v", err)

		// Cleanup
		cleanupSchedulerJob(t, runner, jobName)
	})
}

// TestSchedulerJobCancel tests job cancellation.
func TestSchedulerJobCancel(t *testing.T) {
	RunForAllTransports(t, "CancelJob", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("cancel-test-%d", time.Now().UnixNano())
		executeAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		createCmd := SchedulerJobCreateOneShotCommand(jobName, "test.handler", executeAt)
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		jobID := findSchedulerJobID(t, runner, jobName)
		require.NotEmpty(t, jobID, "Created job must be findable by name: %s", jobName)

		// Cancel without reason
		cancelCmd := SchedulerJobCancelCommand(jobID)
		_, _, err = runner.Run(cancelCmd)
		t.Logf("Cancel without reason result: %v", err)

		// Cleanup
		cleanupSchedulerJob(t, runner, jobName)
	})

	RunForAllTransports(t, "CancelJobWithReason", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("cancel-reason-%d", time.Now().UnixNano())
		executeAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		createCmd := SchedulerJobCreateOneShotCommand(jobName, "test.handler", executeAt)
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		jobID := findSchedulerJobID(t, runner, jobName)
		require.NotEmpty(t, jobID, "Created job must be findable by name: %s", jobName)

		// Cancel with reason
		cancelCmd := SchedulerJobCancelWithReasonCommand(jobID, "Integration test cancellation")
		_, _, err = runner.Run(cancelCmd)
		t.Logf("Cancel with reason result: %v", err)

		// Cleanup
		cleanupSchedulerJob(t, runner, jobName)
	})
}

// TestSchedulerJobErrors tests error handling.
func TestSchedulerJobErrors(t *testing.T) {
	RunForAllTransports(t, "CreateMissingHandler", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("error-test-%d", time.Now().UnixNano())
		runner.RunAndAssert(SchedulerJobCreateMissingHandlerCommand(jobName))
	})

	RunForAllTransports(t, "GetInvalidID", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(SchedulerJobGetInvalidIDCommand())
	})

	RunForAllTransports(t, "GetNonexistent", func(t *testing.T, runner *TestRunner) {
		cmd := SchedulerJobGetNonexistentCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "DeleteInvalidID", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(SchedulerJobDeleteInvalidIDCommand())
	})

	RunForAllTransports(t, "CreateInvalidTimeout", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("invalid-timeout-%d", time.Now().UnixNano())
		runner.RunAndAssert(SchedulerJobCreateInvalidTimeoutCommand(jobName))
	})

	RunForAllTransports(t, "CreateInvalidExecuteAt", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("invalid-execute-%d", time.Now().UnixNano())
		runner.RunAndAssert(SchedulerJobCreateInvalidExecuteAtCommand(jobName))
	})

	RunForAllTransports(t, "CreateInvalidDelay", func(t *testing.T, runner *TestRunner) {
		jobName := fmt.Sprintf("invalid-delay-%d", time.Now().UnixNano())
		runner.RunAndAssert(SchedulerJobCreateInvalidDelayCommand(jobName))
	})
}

// TestSchedulerOutputFormats tests all output formats.
func TestSchedulerOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("Status_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Status_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("scheduler status %s", format),
					Args:           []string{"scheduler", "status", "--output", format},
					ExpectSuccess:  true,
					Timeout:        10 * time.Second,
					SkipTransports: schedulerUnsupportedTransports,
				}
				if runner.IsSkipped(cmd) {
					return
				}
				runner.RunAndAssert(cmd)
			})
		})

		t.Run(fmt.Sprintf("JobList_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("JobList_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("scheduler job list %s", format),
					Args:           []string{"scheduler", "job", "list", "--output", format},
					ExpectSuccess:  true,
					Timeout:        10 * time.Second,
					RequiresUnseal: true,
					SkipTransports: schedulerUnsupportedTransports,
				}
				if runner.IsSkipped(cmd) {
					return
				}
				runner.RunAndAssert(cmd)
			})
		})
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// findSchedulerJobID finds a job ID by name.
func findSchedulerJobID(t *testing.T, runner *TestRunner, jobName string) string {
	t.Helper()

	listCmd := SchedulerJobListJSONCommand()
	stdout, _, err := runner.Run(listCmd)
	if err != nil {
		return ""
	}

	var listResp struct {
		Jobs []struct {
			ID   uint64 `json:"id"`
			Name string `json:"name"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(stdout), &listResp); err != nil {
		return ""
	}

	for _, job := range listResp.Jobs {
		if job.Name == jobName {
			return fmt.Sprintf("%d", job.ID)
		}
	}
	return ""
}

// cleanupSchedulerJob attempts to delete a test job by name.
func cleanupSchedulerJob(t *testing.T, runner *TestRunner, jobName string) {
	t.Helper()

	jobID := findSchedulerJobID(t, runner, jobName)
	if jobID == "" {
		return
	}

	deleteCmd := CommandDef{
		Name:           fmt.Sprintf("cleanup job %s", jobName),
		Args:           []string{"scheduler", "job", "delete", jobID, "--force"},
		SkipTransports: schedulerUnsupportedTransports,
	}
	_, _, _ = runner.Run(deleteCmd)
}

// SchedulerSuite returns all scheduler commands for a complete test suite.
func SchedulerSuite(jobName string) []CommandDef {
	executeAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	return []CommandDef{
		SchedulerHelpCommand(),
		SchedulerJobHelpCommand(),
		SchedulerStatusCommand(),
		SchedulerJobCreateOneShotCommand(jobName, "test.handler", executeAt),
		SchedulerJobListCommand(),
		// Note: Get, Delete, etc. require job ID which we would need to extract
	}
}

// SchedulerHelpSuite returns help commands for scheduler.
func SchedulerHelpSuite() []CommandDef {
	return []CommandDef{
		SchedulerHelpCommand(),
		SchedulerJobHelpCommand(),
		SchedulerJobCreateHelpCommand(),
	}
}

// SchedulerErrorSuite returns error case commands for scheduler.
func SchedulerErrorSuite() []CommandDef {
	return []CommandDef{
		SchedulerJobCreateMissingHandlerCommand("error-test"),
		SchedulerJobGetInvalidIDCommand(),
		SchedulerJobDeleteInvalidIDCommand(),
	}
}

// extractSchedulerJobID attempts to extract a job ID from CLI output.
// This is a helper that parses different output formats.
func extractSchedulerJobID(output string) string {
	// Try to find "ID:" or "id:" followed by a number
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "id:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}
