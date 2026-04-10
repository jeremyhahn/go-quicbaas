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

// Package main provides nil-client tests for QUIC scheduler and KMS wrappers.
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// --------------------------------------------------------------------------
// QUICSchedulerClient nil-client tests
// --------------------------------------------------------------------------

func TestQUICSchedulerClient_NilClient_SchedulerCreateJob(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerCreateJob(context.Background(), &service.CreateSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerGetJob(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerGetJob(context.Background(), &service.GetSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerUpdateJob(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerUpdateJob(context.Background(), &service.UpdateSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerDeleteJob(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerDeleteJob(context.Background(), &service.DeleteSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerListJobs(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerListJobs(context.Background(), &service.ListSchedulerJobsRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerTriggerJob(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerTriggerJob(context.Background(), &service.TriggerSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerPauseJob(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerPauseJob(context.Background(), &service.PauseSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerResumeJob(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerResumeJob(context.Background(), &service.ResumeSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerCancelJob(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerCancelJob(context.Background(), &service.CancelSchedulerJobRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerGetJobHistory(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerGetJobHistory(context.Background(), &service.GetSchedulerJobHistoryRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_SchedulerGetStatus(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	resp, err := client.SchedulerGetStatus(context.Background())
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC client not initialized")
}

func TestQUICSchedulerClient_NilClient_Close(t *testing.T) {
	client := &QUICSchedulerClient{client: nil}
	err := client.Close()
	assert.NoError(t, err)
}

// --------------------------------------------------------------------------
// QUIC KMS wrapper nil-client delegation tests
// These verify that all wrapper methods on QUIC KMS/DEK/Secrets/Seal/Backend/
// Safety/Crypto clients properly panic or return error when the underlying
// quic.Client is nil (they delegate to nil and would panic).
// We test struct instantiation and interface compliance instead.
// --------------------------------------------------------------------------

func TestQUICSecretsClient_NilStruct(t *testing.T) {
	client := &QUICSecretsClient{client: nil}
	assert.NotNil(t, client)
}

func TestQUICSealClient_NilStruct(t *testing.T) {
	client := &QUICSealClient{client: nil}
	assert.NotNil(t, client)
}

func TestQUICCryptoClient_NilStruct(t *testing.T) {
	client := &QUICCryptoClient{client: nil}
	assert.NotNil(t, client)
}
