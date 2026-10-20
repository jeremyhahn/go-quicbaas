// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
)

// progressKMSService answers the DEK progress verbs with a progress whose
// Status and EstimatedCompletion are both set.
type progressKMSService struct {
	mockKMSService
	progress *service.ReencryptionProgress
}

func (m *progressKMSService) GetDEKStatus(_ context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return &service.GetDEKStatusResponse{
		DEK:                  &service.DEKMetadata{Path: req.Path},
		ReencryptionProgress: m.progress,
	}, nil
}

func (m *progressKMSService) GetReencryptionProgress(_ context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return &service.GetReencryptionProgressResponse{Path: req.Path, Status: m.progress.Status, Progress: m.progress}, nil
}

func TestDEKClientOverEmbedded_ProgressCarriesStatusAndEstimatedCompletion(t *testing.T) {
	eta := time.Unix(1700003600, 0).UTC()
	kms := &progressKMSService{progress: &service.ReencryptionProgress{
		Status: "running", InProgress: true, TotalKeys: 4, ProcessedKeys: 2, EstimatedCompletion: &eta,
	}}
	tr, err := New(&mockQuicBaaS{kmsService: kms})
	require.NoError(t, err)
	client := dekclient.NewClient(tr)
	ctx := context.Background()

	resp, err := client.GetReencryptionProgress(ctx, &dekclient.GetReencryptionProgressRequest{Path: "app/key"})
	require.NoError(t, err)
	require.NotNil(t, resp.Progress)
	assert.Equal(t, "running", resp.Progress.Status)
	assert.True(t, resp.Progress.EstimatedCompletion.Equal(eta), "EstimatedCompletion = %v", resp.Progress.EstimatedCompletion)

	status, err := client.GetDEKStatus(ctx, "app/key")
	require.NoError(t, err)
	require.NotNil(t, status.ReencryptionProgress)
	assert.Equal(t, "running", status.ReencryptionProgress.Status)
	assert.True(t, status.ReencryptionProgress.EstimatedCompletion.Equal(eta),
		"EstimatedCompletion = %v", status.ReencryptionProgress.EstimatedCompletion)
}

func TestDEKClientOverEmbedded_ProgressWithoutKMSIsNotConfigured(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)

	_, err = dekclient.NewClient(tr).GetReencryptionProgress(context.Background(),
		&dekclient.GetReencryptionProgressRequest{Path: "app/key"})
	require.Error(t, err)
}
