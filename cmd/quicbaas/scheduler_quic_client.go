//go:build quic

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

// Package main provides QUIC scheduler client implementation for the CLI.
package main

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// QUICSchedulerClient wraps the QUIC client to implement the SchedulerClient interface.
type QUICSchedulerClient struct {
	client *quic.Client
}

// SchedulerCreateJob creates a new scheduled job via QUIC.
func (c *QUICSchedulerClient) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerCreateJob", "QUIC client not initialized")
	}
	return c.client.SchedulerCreateJob(ctx, req)
}

// SchedulerGetJob retrieves a job by ID via QUIC.
func (c *QUICSchedulerClient) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerGetJob", "QUIC client not initialized")
	}
	return c.client.SchedulerGetJob(ctx, req)
}

// SchedulerUpdateJob updates an existing job via QUIC.
func (c *QUICSchedulerClient) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerUpdateJob", "QUIC client not initialized")
	}
	return c.client.SchedulerUpdateJob(ctx, req)
}

// SchedulerDeleteJob deletes a job via QUIC.
func (c *QUICSchedulerClient) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerDeleteJob", "QUIC client not initialized")
	}
	return c.client.SchedulerDeleteJob(ctx, req)
}

// SchedulerListJobs lists all jobs matching filters via QUIC.
func (c *QUICSchedulerClient) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerListJobs", "QUIC client not initialized")
	}
	return c.client.SchedulerListJobs(ctx, req)
}

// SchedulerTriggerJob triggers immediate execution of a job via QUIC.
func (c *QUICSchedulerClient) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerTriggerJob", "QUIC client not initialized")
	}
	return c.client.SchedulerTriggerJob(ctx, req)
}

// SchedulerPauseJob pauses a job via QUIC.
func (c *QUICSchedulerClient) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerPauseJob", "QUIC client not initialized")
	}
	return c.client.SchedulerPauseJob(ctx, req)
}

// SchedulerResumeJob resumes a paused job via QUIC.
func (c *QUICSchedulerClient) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerResumeJob", "QUIC client not initialized")
	}
	return c.client.SchedulerResumeJob(ctx, req)
}

// SchedulerCancelJob cancels a job via QUIC.
func (c *QUICSchedulerClient) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerCancelJob", "QUIC client not initialized")
	}
	return c.client.SchedulerCancelJob(ctx, req)
}

// SchedulerGetJobHistory retrieves job execution history via QUIC.
func (c *QUICSchedulerClient) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerGetJobHistory", "QUIC client not initialized")
	}
	return c.client.SchedulerGetJobHistory(ctx, req)
}

// SchedulerGetStatus retrieves scheduler status via QUIC.
func (c *QUICSchedulerClient) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "QUICSchedulerClient.SchedulerGetStatus", "QUIC client not initialized")
	}
	return c.client.SchedulerGetStatus(ctx)
}

// Close closes the QUIC client connection.
func (c *QUICSchedulerClient) Close() error {
	if c.client == nil {
		return nil
	}
	return c.client.Close()
}

// createSchedulerQUICClientFactory creates a QUIC client for scheduler operations.
// QUIC transport for scheduler commands uses QUIC protocol for low-latency, multiplexed connections.
func createSchedulerQUICClientFactory() (SchedulerClient, error) {
	logCfg := logger.LogConfig{
		Level:     getSchedulerLogLevel(),
		Format:    "json",
		Output:    "stderr",
		Component: "quic-scheduler-client",
	}
	log, err := logger.New(logCfg)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrConfiguration,
			"createSchedulerQUICClientFactory",
			"failed to create logger",
		)
	}

	opts := []quic.ClientOption{
		quic.WithLogger(log),
		quic.WithTimeout(timeout),
		quic.WithMaxIdleTimeout(5 * time.Minute),
	}

	// TLS is required for QUIC connections
	if !tlsEnabled && !tlsSkipVerify {
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createSchedulerQUICClientFactory",
			"TLS is required for QUIC connections",
		)
	}

	if tlsSkipVerify {
		opts = append(opts, quic.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // G402: user explicitly requested --tls-skip-verify
			MinVersion:         tls.VersionTLS13,
		}))
	}

	// Create QUIC client
	quicClient, err := quic.NewClient(address, opts...)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"createSchedulerQUICClientFactory",
			"failed to create QUIC client for server %s", address,
		)
	}

	return &QUICSchedulerClient{client: quicClient}, nil
}

// Compile-time interface check.
var _ SchedulerClient = (*QUICSchedulerClient)(nil)
