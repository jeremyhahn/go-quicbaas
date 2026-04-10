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

// Package scheduler provides the job scheduler client for the QuicBaaS SDK.
//
// The scheduler client supports creating, managing, and monitoring scheduled
// jobs including one-shot, recurring, and delayed execution patterns.
//
// # Creating a Job
//
//	client := scheduler.NewClient(transport)
//
//	// Create a recurring job that runs every hour
//	resp, err := client.CreateJob(ctx, &scheduler.CreateJobRequest{
//	    Name:        "hourly-backup",
//	    Description: "Perform hourly database backup",
//	    JobType:     scheduler.JobTypeRecurring,
//	    Schedule:    "0 * * * *", // cron expression
//	    Payload:     []byte(`{"target": "database"}`),
//	    OwnerType:   scheduler.OwnerTypeSystem,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	jobID := resp.JobID
//
// # Job Types
//
// The scheduler supports three job types:
//   - JobTypeOneShot: Execute once and complete
//   - JobTypeRecurring: Execute on a cron schedule
//   - JobTypeDelayed: Execute after a specified delay
//
// # Job Status
//
// Jobs progress through the following statuses:
//   - JobStatusPending: Job is scheduled but not yet running
//   - JobStatusRunning: Job is currently executing
//   - JobStatusCompleted: Job finished successfully
//   - JobStatusFailed: Job execution failed
//   - JobStatusPaused: Job is paused and will not execute
//   - JobStatusCanceled: Job has been canceled
//
// # Owner Types
//
// Jobs can be owned by different entity types:
//   - OwnerTypeSystem: System-level jobs
//   - OwnerTypeTenant: Tenant-specific jobs
//   - OwnerTypeDelegated: Jobs delegated to another entity
//
// # Job Management
//
// Jobs can be paused, resumed, triggered immediately, or canceled:
//
//	// Pause a job
//	_, err := client.PauseJob(ctx, jobID)
//
//	// Resume a paused job
//	_, err := client.ResumeJob(ctx, jobID)
//
//	// Trigger immediate execution
//	_, err := client.TriggerJob(ctx, jobID)
//
//	// Cancel with reason
//	_, err := client.CancelJob(ctx, jobID, "no longer needed")
//
// # Execution History
//
// Retrieve job execution history for monitoring and debugging:
//
//	history, err := client.GetJobHistory(ctx, jobID, &scheduler.HistoryOptions{
//	    Limit:  100,
//	    Offset: 0,
//	})
//	for _, exec := range history.Executions {
//	    fmt.Printf("Execution %d: %s (duration: %v)\n",
//	        exec.ExecutionID, exec.Status, exec.Duration)
//	}
package scheduler
