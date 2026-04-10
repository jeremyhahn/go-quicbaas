//go:build rest

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

// Package main provides REST scheduler client implementation for the CLI.
package main

import (
	"context"
	"crypto/tls"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// RESTSchedulerClient wraps the REST client to implement the SchedulerClient interface.
type RESTSchedulerClient struct {
	client *rest.RESTClient
}

// SchedulerCreateJob creates a new scheduled job via REST API.
func (c *RESTSchedulerClient) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerCreateJob", "REST client not initialized")
	}
	return c.client.SchedulerCreateJob(ctx, req)
}

// SchedulerGetJob retrieves a job by ID via REST API.
func (c *RESTSchedulerClient) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerGetJob", "REST client not initialized")
	}
	return c.client.SchedulerGetJob(ctx, req)
}

// SchedulerUpdateJob updates an existing job via REST API.
func (c *RESTSchedulerClient) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerUpdateJob", "REST client not initialized")
	}
	return c.client.SchedulerUpdateJob(ctx, req)
}

// SchedulerDeleteJob deletes a job via REST API.
func (c *RESTSchedulerClient) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerDeleteJob", "REST client not initialized")
	}
	return c.client.SchedulerDeleteJob(ctx, req)
}

// SchedulerListJobs lists all jobs matching filters via REST API.
func (c *RESTSchedulerClient) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerListJobs", "REST client not initialized")
	}
	return c.client.SchedulerListJobs(ctx, req)
}

// SchedulerTriggerJob triggers immediate execution of a job via REST API.
func (c *RESTSchedulerClient) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerTriggerJob", "REST client not initialized")
	}
	return c.client.SchedulerTriggerJob(ctx, req)
}

// SchedulerPauseJob pauses a job via REST API.
func (c *RESTSchedulerClient) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerPauseJob", "REST client not initialized")
	}
	return c.client.SchedulerPauseJob(ctx, req)
}

// SchedulerResumeJob resumes a paused job via REST API.
func (c *RESTSchedulerClient) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerResumeJob", "REST client not initialized")
	}
	return c.client.SchedulerResumeJob(ctx, req)
}

// SchedulerCancelJob cancels a job via REST API.
func (c *RESTSchedulerClient) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerCancelJob", "REST client not initialized")
	}
	return c.client.SchedulerCancelJob(ctx, req)
}

// SchedulerGetJobHistory retrieves job execution history via REST API.
func (c *RESTSchedulerClient) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerGetJobHistory", "REST client not initialized")
	}
	return c.client.SchedulerGetJobHistory(ctx, req)
}

// SchedulerGetStatus retrieves scheduler status via REST API.
func (c *RESTSchedulerClient) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	if c.client == nil {
		return nil, errors.New(errors.ErrUnavailable, "RESTSchedulerClient.SchedulerGetStatus", "REST client not initialized")
	}
	return c.client.SchedulerGetStatus(ctx)
}

// Close closes the REST client connection.
func (c *RESTSchedulerClient) Close() error {
	if c.client == nil {
		return nil
	}
	return c.client.Close()
}

// createSchedulerRESTClientFactory creates a REST client for scheduler operations.
// REST transport for scheduler commands uses HTTP/REST calls to the server's scheduler API endpoints.
func createSchedulerRESTClientFactory() (SchedulerClient, error) {
	// Create logger for the REST client
	logCfg := logger.LogConfig{
		Level:     getSchedulerLogLevel(),
		Format:    "json",
		Output:    "stderr",
		Component: "rest-scheduler-client",
	}
	log, err := logger.New(logCfg)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrConfiguration,
			"createSchedulerRESTClientFactory",
			"failed to create logger",
		)
	}

	// Construct base URL from address
	baseURL := constructSchedulerRESTBaseURL(address)

	// Build REST client options
	var opts []rest.ClientOption

	// Set timeout
	if timeout > 0 {
		opts = append(opts, rest.WithTimeout(timeout))
	}

	// Configure TLS if enabled
	if tlsEnabled {
		if tlsCert != "" && tlsKey != "" {
			opts = append(opts, rest.WithTLS(tlsCert, tlsKey, tlsCA))
		} else if tlsCA != "" {
			opts = append(opts, rest.WithTLS("", "", tlsCA))
		}
	}
	if tlsSkipVerify {
		opts = append(opts, rest.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // G402: user explicitly requested --tls-skip-verify
			MinVersion:         tls.VersionTLS13,
		}))
	}

	// Create the REST client
	restClient, err := rest.NewRESTClient(baseURL, log, opts...)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"createSchedulerRESTClientFactory",
			"failed to create REST client for server %s", address,
		)
	}

	return &RESTSchedulerClient{client: restClient}, nil
}

// Compile-time interface check.
var _ SchedulerClient = (*RESTSchedulerClient)(nil)
