// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a mock transport for testing.
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) Healthy() bool {
	return true
}

func TestNewClient(t *testing.T) {
	t.Run("with valid transport", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		client := NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

func TestClient_CreateJob_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &CreateJobResponse{JobID: 123, NextRunAt: time.Now().Add(time.Hour)}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *CreateJobRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &CreateJobRequest{JobType: JobTypeOneShot, OwnerType: OwnerTypeSystem},
			wantErr: true,
		},
		{
			name: "invalid job type",
			req: &CreateJobRequest{
				Name:      "test-job",
				JobType:   JobType(99),
				OwnerType: OwnerTypeSystem,
			},
			wantErr: true,
		},
		{
			name: "invalid owner type",
			req: &CreateJobRequest{
				Name:      "test-job",
				JobType:   JobTypeOneShot,
				OwnerType: OwnerType(99),
			},
			wantErr: true,
		},
		{
			name: "recurring without schedule",
			req: &CreateJobRequest{
				Name:      "test-job",
				JobType:   JobTypeRecurring,
				OwnerType: OwnerTypeSystem,
			},
			wantErr: true,
		},
		{
			name: "delayed without delay",
			req: &CreateJobRequest{
				Name:      "test-job",
				JobType:   JobTypeDelayed,
				OwnerType: OwnerTypeSystem,
			},
			wantErr: true,
		},
		{
			name: "negative max retries",
			req: &CreateJobRequest{
				Name:       "test-job",
				JobType:    JobTypeOneShot,
				OwnerType:  OwnerTypeSystem,
				MaxRetries: -1,
			},
			wantErr: true,
		},
		{
			name: "negative timeout",
			req: &CreateJobRequest{
				Name:      "test-job",
				JobType:   JobTypeOneShot,
				OwnerType: OwnerTypeSystem,
				Timeout:   -time.Second,
			},
			wantErr: true,
		},
		{
			name: "valid one-shot job",
			req: &CreateJobRequest{
				Name:      "test-job",
				JobType:   JobTypeOneShot,
				OwnerType: OwnerTypeSystem,
			},
			wantErr: false,
		},
		{
			name: "valid recurring job",
			req: &CreateJobRequest{
				Name:      "recurring-job",
				JobType:   JobTypeRecurring,
				Schedule:  "0 * * * *",
				OwnerType: OwnerTypeTenant,
				OwnerID:   "tenant-123",
			},
			wantErr: false,
		},
		{
			name: "valid delayed job",
			req: &CreateJobRequest{
				Name:      "delayed-job",
				JobType:   JobTypeDelayed,
				Delay:     5 * time.Minute,
				OwnerType: OwnerTypeDelegated,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.CreateJob(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_CreateJob(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful creation",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "CreateJob" {
						t.Errorf("expected method %q, got %q", "CreateJob", method)
					}
					return &CreateJobResponse{
						JobID:     123,
						NextRunAt: time.Now().Add(time.Hour),
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &CreateJobRequest{
		Name:      "test-job",
		JobType:   JobTypeOneShot,
		OwnerType: OwnerTypeSystem,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.CreateJob(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GetJob(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		jobID     uint64
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:  "successful get",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetJob" {
						t.Errorf("expected method %q, got %q", "GetJob", method)
					}
					return &JobInfo{
						JobID:     123,
						Name:      "test-job",
						JobType:   JobTypeRecurring,
						Status:    JobStatusPending,
						OwnerType: OwnerTypeSystem,
						CreatedAt: now,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "zero job ID",
			ctx:       context.Background(),
			jobID:     0,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			jobID:     123,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "job not found",
			ctx:   context.Background(),
			jobID: 999,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetJob(tt.ctx, tt.jobID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_UpdateJob_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &JobInfo{JobID: 123, Name: "updated-job"}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *UpdateJobRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "zero job ID",
			req:     &UpdateJobRequest{JobID: 0, Name: "test"},
			wantErr: true,
		},
		{
			name:    "negative max retries",
			req:     &UpdateJobRequest{JobID: 123, MaxRetries: -1},
			wantErr: true,
		},
		{
			name:    "negative timeout",
			req:     &UpdateJobRequest{JobID: 123, Timeout: -time.Second},
			wantErr: true,
		},
		{
			name:    "valid update",
			req:     &UpdateJobRequest{JobID: 123, Name: "updated-job"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.UpdateJob(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_UpdateJob(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful update",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "UpdateJob" {
						t.Errorf("expected method %q, got %q", "UpdateJob", method)
					}
					return &JobInfo{JobID: 123, Name: "updated-job"}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &UpdateJobRequest{JobID: 123, Name: "updated-job"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.UpdateJob(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_DeleteJob(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		jobID     uint64
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:  "successful delete",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "DeleteJob" {
						t.Errorf("expected method %q, got %q", "DeleteJob", method)
					}
					return &deleteJobResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name:  "successful delete with nil response",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "zero job ID",
			ctx:       context.Background(),
			jobID:     0,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			jobID:     123,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "job not found",
			ctx:   context.Background(),
			jobID: 999,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "job not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.DeleteJob(tt.ctx, tt.jobID)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestClient_ListJobs_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListJobsResponse{Jobs: []JobInfo{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	invalidOwnerType := OwnerType(99)
	invalidStatus := JobStatus(99)
	invalidJobType := JobType(99)
	validOwnerType := OwnerTypeSystem
	validStatus := JobStatusPending
	validJobType := JobTypeRecurring

	tests := []struct {
		name    string
		opts    *ListJobsOptions
		wantErr bool
	}{
		{
			name:    "nil options",
			opts:    nil,
			wantErr: false,
		},
		{
			name:    "invalid owner type",
			opts:    &ListJobsOptions{OwnerType: &invalidOwnerType},
			wantErr: true,
		},
		{
			name:    "invalid status",
			opts:    &ListJobsOptions{Status: &invalidStatus},
			wantErr: true,
		},
		{
			name:    "invalid job type",
			opts:    &ListJobsOptions{JobType: &invalidJobType},
			wantErr: true,
		},
		{
			name:    "negative limit",
			opts:    &ListJobsOptions{Limit: -1},
			wantErr: true,
		},
		{
			name:    "negative offset",
			opts:    &ListJobsOptions{Offset: -1},
			wantErr: true,
		},
		{
			name: "valid options",
			opts: &ListJobsOptions{
				OwnerType: &validOwnerType,
				Status:    &validStatus,
				JobType:   &validJobType,
				Limit:     10,
				Offset:    0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ListJobs(ctx, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListJobs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_ListJobs(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		jobCount  int
	}{
		{
			name: "successful list",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ListJobs" {
						t.Errorf("expected method %q, got %q", "ListJobs", method)
					}
					return &ListJobsResponse{
						Jobs: []JobInfo{
							{JobID: 1, Name: "job-one"},
							{JobID: 2, Name: "job-two"},
						},
						TotalCount: 2,
						HasMore:    false,
					}, nil
				},
			},
			wantErr:  false,
			jobCount: 2,
		},
		{
			name: "empty list with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:  false,
			jobCount: 0,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ListJobs(tt.ctx, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListJobs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(resp.Jobs) != tt.jobCount {
				t.Errorf("expected %d jobs, got %d", tt.jobCount, len(resp.Jobs))
			}
		})
	}
}

func TestClient_TriggerJob(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		jobID     uint64
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:  "successful trigger",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "TriggerJob" {
						t.Errorf("expected method %q, got %q", "TriggerJob", method)
					}
					return &TriggerJobResponse{
						JobID:       123,
						ExecutionID: 456,
						StartedAt:   now,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "zero job ID",
			ctx:       context.Background(),
			jobID:     0,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			jobID:     123,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "nil response",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.TriggerJob(tt.ctx, tt.jobID)
			if (err != nil) != tt.wantErr {
				t.Errorf("TriggerJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_PauseJob(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		jobID     uint64
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:  "successful pause",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "PauseJob" {
						t.Errorf("expected method %q, got %q", "PauseJob", method)
					}
					return &PauseJobResponse{JobID: 123, PausedAt: now}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "zero job ID",
			ctx:       context.Background(),
			jobID:     0,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			jobID:     123,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "nil response",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.PauseJob(tt.ctx, tt.jobID)
			if (err != nil) != tt.wantErr {
				t.Errorf("PauseJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_ResumeJob(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		jobID     uint64
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:  "successful resume",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ResumeJob" {
						t.Errorf("expected method %q, got %q", "ResumeJob", method)
					}
					return &ResumeJobResponse{
						JobID:     123,
						ResumedAt: now,
						NextRunAt: now.Add(time.Hour),
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "zero job ID",
			ctx:       context.Background(),
			jobID:     0,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			jobID:     123,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "nil response",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ResumeJob(tt.ctx, tt.jobID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResumeJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_CancelJob(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		jobID     uint64
		reason    string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:   "successful cancel with reason",
			ctx:    context.Background(),
			jobID:  123,
			reason: "no longer needed",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "CancelJob" {
						t.Errorf("expected method %q, got %q", "CancelJob", method)
					}
					cancelReq := req.(*cancelJobRequest)
					if cancelReq.Reason != "no longer needed" {
						t.Errorf("expected reason 'no longer needed', got %q", cancelReq.Reason)
					}
					return &CancelJobResponse{
						JobID:      123,
						CanceledAt: now,
						Reason:     "no longer needed",
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:   "successful cancel without reason",
			ctx:    context.Background(),
			jobID:  123,
			reason: "",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &CancelJobResponse{JobID: 123, CanceledAt: now}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "zero job ID",
			ctx:       context.Background(),
			jobID:     0,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			jobID:     123,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "nil response",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.CancelJob(tt.ctx, tt.jobID, tt.reason)
			if (err != nil) != tt.wantErr {
				t.Errorf("CancelJob() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GetJobHistory_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &JobHistoryResponse{JobID: 123, Executions: []JobExecution{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	startTime := time.Now().Add(-time.Hour)
	endTime := time.Now()
	invalidStatus := JobStatus(99)
	validStatus := JobStatusCompleted

	tests := []struct {
		name    string
		jobID   uint64
		opts    *HistoryOptions
		wantErr bool
	}{
		{
			name:    "nil options",
			jobID:   123,
			opts:    nil,
			wantErr: false,
		},
		{
			name:    "negative limit",
			jobID:   123,
			opts:    &HistoryOptions{Limit: -1},
			wantErr: true,
		},
		{
			name:    "negative offset",
			jobID:   123,
			opts:    &HistoryOptions{Offset: -1},
			wantErr: true,
		},
		{
			name:    "start time after end time",
			jobID:   123,
			opts:    &HistoryOptions{StartTime: &endTime, EndTime: &startTime},
			wantErr: true,
		},
		{
			name:    "invalid status",
			jobID:   123,
			opts:    &HistoryOptions{Status: &invalidStatus},
			wantErr: true,
		},
		{
			name:  "valid options",
			jobID: 123,
			opts: &HistoryOptions{
				Limit:     100,
				Offset:    0,
				StartTime: &startTime,
				EndTime:   &endTime,
				Status:    &validStatus,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetJobHistory(ctx, tt.jobID, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetJobHistory() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GetJobHistory(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		jobID     uint64
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		execCount int
	}{
		{
			name:  "successful get history",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetJobHistory" {
						t.Errorf("expected method %q, got %q", "GetJobHistory", method)
					}
					return &JobHistoryResponse{
						JobID: 123,
						Executions: []JobExecution{
							{ExecutionID: 1, JobID: 123, Status: JobStatusCompleted, StartedAt: now.Add(-2 * time.Hour)},
							{ExecutionID: 2, JobID: 123, Status: JobStatusCompleted, StartedAt: now.Add(-time.Hour)},
						},
						TotalCount: 2,
					}, nil
				},
			},
			wantErr:   false,
			execCount: 2,
		},
		{
			name:  "empty history with nil response",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:   false,
			execCount: 0,
		},
		{
			name:      "zero job ID",
			ctx:       context.Background(),
			jobID:     0,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			jobID:     123,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			jobID: 123,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetJobHistory(tt.ctx, tt.jobID, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetJobHistory() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(resp.Executions) != tt.execCount {
				t.Errorf("expected %d executions, got %d", tt.execCount, len(resp.Executions))
			}
		})
	}
}

func TestClient_GetStatus(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful get status",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetStatus" {
						t.Errorf("expected method %q, got %q", "GetStatus", method)
					}
					return &SchedulerStatus{
						Healthy:        true,
						TotalJobs:      100,
						PendingJobs:    20,
						RunningJobs:    5,
						CompletedJobs:  70,
						FailedJobs:     3,
						PausedJobs:     2,
						WorkerCount:    4,
						QueueDepth:     10,
						LastHeartbeat:  now,
						UptimeDuration: 24 * time.Hour,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "scheduler unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetStatus(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestJobType_String(t *testing.T) {
	tests := []struct {
		jobType  JobType
		expected string
	}{
		{JobTypeOneShot, "ONE_SHOT"},
		{JobTypeRecurring, "RECURRING"},
		{JobTypeDelayed, "DELAYED"},
		{JobType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.jobType.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestJobStatus_String(t *testing.T) {
	tests := []struct {
		status   JobStatus
		expected string
	}{
		{JobStatusPending, "PENDING"},
		{JobStatusRunning, "RUNNING"},
		{JobStatusCompleted, "COMPLETED"},
		{JobStatusFailed, "FAILED"},
		{JobStatusPaused, "PAUSED"},
		{JobStatusCanceled, "CANCELED"},
		{JobStatus(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestOwnerType_String(t *testing.T) {
	tests := []struct {
		ownerType OwnerType
		expected  string
	}{
		{OwnerTypeSystem, "SYSTEM"},
		{OwnerTypeTenant, "TENANT"},
		{OwnerTypeDelegated, "DELEGATED"},
		{OwnerType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.ownerType.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestJobInfo_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &JobInfo{
				JobID:          123,
				Name:           "test-job",
				Description:    "A test job",
				JobType:        JobTypeRecurring,
				Status:         JobStatusPending,
				Schedule:       "0 * * * *",
				Payload:        []byte(`{"key": "value"}`),
				OwnerType:      OwnerTypeTenant,
				OwnerID:        "tenant-456",
				MaxRetries:     3,
				Timeout:        5 * time.Minute,
				NextRunAt:      now.Add(time.Hour),
				CreatedAt:      now.Add(-24 * time.Hour),
				UpdatedAt:      now,
				ExecutionCount: 100,
				FailureCount:   2,
				Tags:           map[string]string{"env": "prod"},
			}, nil
		},
	}

	client := NewClient(mockT)
	info, err := client.GetJob(context.Background(), 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.JobID != 123 {
		t.Errorf("expected JobID=123, got %d", info.JobID)
	}
	if info.Name != "test-job" {
		t.Errorf("expected Name='test-job', got %q", info.Name)
	}
	if info.Description != "A test job" {
		t.Errorf("expected Description='A test job', got %q", info.Description)
	}
	if info.JobType != JobTypeRecurring {
		t.Errorf("expected JobType=RECURRING, got %v", info.JobType)
	}
	if info.Status != JobStatusPending {
		t.Errorf("expected Status=PENDING, got %v", info.Status)
	}
	if info.Schedule != "0 * * * *" {
		t.Errorf("expected Schedule='0 * * * *', got %q", info.Schedule)
	}
	if info.OwnerType != OwnerTypeTenant {
		t.Errorf("expected OwnerType=TENANT, got %v", info.OwnerType)
	}
	if info.OwnerID != "tenant-456" {
		t.Errorf("expected OwnerID='tenant-456', got %q", info.OwnerID)
	}
	if info.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", info.MaxRetries)
	}
	if info.ExecutionCount != 100 {
		t.Errorf("expected ExecutionCount=100, got %d", info.ExecutionCount)
	}
	if info.Tags["env"] != "prod" {
		t.Errorf("expected Tags[env]='prod', got %q", info.Tags["env"])
	}
}

func TestJobExecution_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &JobHistoryResponse{
				JobID: 123,
				Executions: []JobExecution{
					{
						ExecutionID: 1,
						JobID:       123,
						Status:      JobStatusCompleted,
						StartedAt:   now.Add(-time.Hour),
						CompletedAt: now.Add(-55 * time.Minute),
						Duration:    5 * time.Minute,
						RetryCount:  0,
						Result:      []byte(`{"success": true}`),
					},
					{
						ExecutionID: 2,
						JobID:       123,
						Status:      JobStatusFailed,
						StartedAt:   now.Add(-2 * time.Hour),
						CompletedAt: now.Add(-115 * time.Minute),
						Duration:    5 * time.Minute,
						RetryCount:  3,
						Error:       "connection timeout",
					},
				},
			}, nil
		},
	}

	client := NewClient(mockT)
	history, err := client.GetJobHistory(context.Background(), 123, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(history.Executions) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(history.Executions))
	}

	exec1 := history.Executions[0]
	if exec1.ExecutionID != 1 {
		t.Errorf("expected ExecutionID=1, got %d", exec1.ExecutionID)
	}
	if exec1.Status != JobStatusCompleted {
		t.Errorf("expected Status=COMPLETED, got %v", exec1.Status)
	}
	if exec1.Duration != 5*time.Minute {
		t.Errorf("expected Duration=5m, got %v", exec1.Duration)
	}

	exec2 := history.Executions[1]
	if exec2.Status != JobStatusFailed {
		t.Errorf("expected Status=FAILED, got %v", exec2.Status)
	}
	if exec2.RetryCount != 3 {
		t.Errorf("expected RetryCount=3, got %d", exec2.RetryCount)
	}
	if exec2.Error != "connection timeout" {
		t.Errorf("expected Error='connection timeout', got %q", exec2.Error)
	}
}

func TestSchedulerStatus_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SchedulerStatus{
				Healthy:         true,
				TotalJobs:       1000,
				PendingJobs:     200,
				RunningJobs:     50,
				CompletedJobs:   700,
				FailedJobs:      30,
				PausedJobs:      15,
				CanceledJobs:    5,
				ExecutionsToday: 500,
				FailuresToday:   10,
				AverageLatency:  100 * time.Millisecond,
				WorkerCount:     8,
				QueueDepth:      25,
				LastHeartbeat:   now,
				UptimeDuration:  7 * 24 * time.Hour,
			}, nil
		},
	}

	client := NewClient(mockT)
	status, err := client.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.Healthy {
		t.Error("expected Healthy=true")
	}
	if status.TotalJobs != 1000 {
		t.Errorf("expected TotalJobs=1000, got %d", status.TotalJobs)
	}
	if status.PendingJobs != 200 {
		t.Errorf("expected PendingJobs=200, got %d", status.PendingJobs)
	}
	if status.RunningJobs != 50 {
		t.Errorf("expected RunningJobs=50, got %d", status.RunningJobs)
	}
	if status.CompletedJobs != 700 {
		t.Errorf("expected CompletedJobs=700, got %d", status.CompletedJobs)
	}
	if status.FailedJobs != 30 {
		t.Errorf("expected FailedJobs=30, got %d", status.FailedJobs)
	}
	if status.WorkerCount != 8 {
		t.Errorf("expected WorkerCount=8, got %d", status.WorkerCount)
	}
	if status.QueueDepth != 25 {
		t.Errorf("expected QueueDepth=25, got %d", status.QueueDepth)
	}
	if status.AverageLatency != 100*time.Millisecond {
		t.Errorf("expected AverageLatency=100ms, got %v", status.AverageLatency)
	}
}

func TestCreateJobRequest_AllJobTypes(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &CreateJobResponse{JobID: 123, NextRunAt: time.Now()}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	// Test all valid job types
	jobTypes := []struct {
		jobType  JobType
		schedule string
		delay    time.Duration
	}{
		{JobTypeOneShot, "", 0},
		{JobTypeRecurring, "0 * * * *", 0},
		{JobTypeDelayed, "", 5 * time.Minute},
	}

	for _, jt := range jobTypes {
		t.Run(jt.jobType.String(), func(t *testing.T) {
			req := &CreateJobRequest{
				Name:      "test-job",
				JobType:   jt.jobType,
				Schedule:  jt.schedule,
				Delay:     jt.delay,
				OwnerType: OwnerTypeSystem,
			}
			resp, err := client.CreateJob(ctx, req)
			if err != nil {
				t.Errorf("CreateJob(%v) unexpected error: %v", jt.jobType, err)
			}
			if resp == nil {
				t.Errorf("CreateJob(%v) returned nil response", jt.jobType)
			}
		})
	}
}

func TestCreateJobRequest_AllOwnerTypes(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &CreateJobResponse{JobID: 123, NextRunAt: time.Now()}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	ownerTypes := []OwnerType{OwnerTypeSystem, OwnerTypeTenant, OwnerTypeDelegated}

	for _, ot := range ownerTypes {
		t.Run(ot.String(), func(t *testing.T) {
			req := &CreateJobRequest{
				Name:      "test-job",
				JobType:   JobTypeOneShot,
				OwnerType: ot,
			}
			resp, err := client.CreateJob(ctx, req)
			if err != nil {
				t.Errorf("CreateJob(ownerType=%v) unexpected error: %v", ot, err)
			}
			if resp == nil {
				t.Errorf("CreateJob(ownerType=%v) returned nil response", ot)
			}
		})
	}
}

func TestListJobsOptions_AllStatusFilters(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListJobsResponse{Jobs: []JobInfo{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	statuses := []JobStatus{
		JobStatusPending, JobStatusRunning, JobStatusCompleted,
		JobStatusFailed, JobStatusPaused, JobStatusCanceled,
	}

	for _, status := range statuses {
		t.Run(status.String(), func(t *testing.T) {
			s := status
			opts := &ListJobsOptions{Status: &s}
			_, err := client.ListJobs(ctx, opts)
			if err != nil {
				t.Errorf("ListJobs(status=%v) unexpected error: %v", status, err)
			}
		})
	}
}

func TestHistoryOptions_AllStatusFilters(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &JobHistoryResponse{JobID: 123, Executions: []JobExecution{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	statuses := []JobStatus{
		JobStatusPending, JobStatusRunning, JobStatusCompleted,
		JobStatusFailed, JobStatusPaused, JobStatusCanceled,
	}

	for _, status := range statuses {
		t.Run(status.String(), func(t *testing.T) {
			s := status
			opts := &HistoryOptions{Status: &s}
			_, err := client.GetJobHistory(ctx, 123, opts)
			if err != nil {
				t.Errorf("GetJobHistory(status=%v) unexpected error: %v", status, err)
			}
		})
	}
}

func TestTransportErrorPropagation(t *testing.T) {
	transportErr := errors.NewConnection("test", "connection refused")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, transportErr
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	// Test error propagation for all methods
	t.Run("CreateJob", func(t *testing.T) {
		_, err := client.CreateJob(ctx, &CreateJobRequest{Name: "test", JobType: JobTypeOneShot, OwnerType: OwnerTypeSystem})
		if err == nil {
			t.Error("expected error")
		}
		if errors.Code(err) != errors.CodeConnection {
			t.Errorf("expected CodeConnection, got %v", errors.Code(err))
		}
	})

	t.Run("GetJob", func(t *testing.T) {
		_, err := client.GetJob(ctx, 123)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("UpdateJob", func(t *testing.T) {
		_, err := client.UpdateJob(ctx, &UpdateJobRequest{JobID: 123})
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("DeleteJob", func(t *testing.T) {
		err := client.DeleteJob(ctx, 123)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("ListJobs", func(t *testing.T) {
		_, err := client.ListJobs(ctx, nil)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("TriggerJob", func(t *testing.T) {
		_, err := client.TriggerJob(ctx, 123)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("PauseJob", func(t *testing.T) {
		_, err := client.PauseJob(ctx, 123)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("ResumeJob", func(t *testing.T) {
		_, err := client.ResumeJob(ctx, 123)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("CancelJob", func(t *testing.T) {
		_, err := client.CancelJob(ctx, 123, "reason")
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("GetJobHistory", func(t *testing.T) {
		_, err := client.GetJobHistory(ctx, 123, nil)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("GetStatus", func(t *testing.T) {
		_, err := client.GetStatus(ctx)
		if err == nil {
			t.Error("expected error")
		}
	})
}
