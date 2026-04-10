package service

import (
	"context"
	"testing"
)

func TestKMSService_ListKeyVersions_NilRequest_Debug(t *testing.T) {
	mockManager := newMockKMSManager()
	logger := newMockLogger()

	// Try without metrics initialization - create service directly
	svc := &kmsServiceImpl{
		kmsManager: mockManager,
		logger:     logger,
		metrics:    nil, // No metrics
	}

	t.Logf("Service type: %T", svc)
	t.Logf("Service metrics: %v", svc.metrics)

	// Call it directly
	nilReq := (*ListKeyVersionsRequest)(nil)
	t.Logf("Calling with nil req: %v", nilReq)

	resp, err := svc.ListKeyVersions(context.Background(), nilReq)

	t.Logf("Response: %+v", resp)
	t.Logf("Error: %v", err)
	t.Logf("Error type: %T", err)

	if err == nil {
		t.Errorf("Expected error for nil request, got nil")
	}
}
