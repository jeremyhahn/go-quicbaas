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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestSchedulerCreateJob tests the SchedulerCreateJob client method.
func TestSchedulerCreateJob(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful job creation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/scheduler/jobs", r.URL.Path)

			now := time.Now()
			resp := CreateSchedulerJobRESTResponse{
				Job: &SchedulerJobRESTResponse{
					ID:          123,
					Name:        "test-job",
					Description: "Test scheduled job",
					OwnerType:   "user",
					OwnerID:     "user-123",
					Type:        "cron",
					Schedule:    "0 * * * *",
					Handler:     "backup",
					Status:      "scheduled",
					Priority:    5,
					MaxRetries:  3,
					CreatedAt:   now,
					UpdatedAt:   now,
				},
				Timestamp: now,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerCreateJob(ctx, &service.CreateSchedulerJobRequest{
			Name:        "test-job",
			Description: "Test scheduled job",
			OwnerType:   "user",
			OwnerID:     "user-123",
			Type:        "cron",
			Schedule:    "0 * * * *",
			Handler:     "backup",
			Priority:    5,
			MaxRetries:  3,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Job)
		assert.Equal(t, uint64(123), resp.Job.ID)
		assert.Equal(t, "test-job", resp.Job.Name)
	})

	t.Run("job creation with execute at", func(t *testing.T) {
		executeAt := time.Now().Add(time.Hour)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req CreateSchedulerJobRESTRequest
			json.NewDecoder(r.Body).Decode(&req)
			assert.NotEmpty(t, req.ExecuteAt)

			now := time.Now()
			resp := CreateSchedulerJobRESTResponse{
				Job: &SchedulerJobRESTResponse{
					ID:        124,
					Name:      "delayed-job",
					Type:      "delayed",
					Handler:   "notification",
					Status:    "pending",
					CreatedAt: now,
				},
				Timestamp: now,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerCreateJob(ctx, &service.CreateSchedulerJobRequest{
			Name:      "delayed-job",
			Type:      "delayed",
			Handler:   "notification",
			ExecuteAt: executeAt,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(124), resp.Job.ID)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerCreateJob(ctx, nil)
		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerCreateJob(ctx, &service.CreateSchedulerJobRequest{
			Name:    "test-job",
			Handler: "backup",
		})
		require.Error(t, err)
	})
}

// TestSchedulerGetJob tests the SchedulerGetJob client method.
func TestSchedulerGetJob(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful job retrieval", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v1/scheduler/jobs/123", r.URL.Path)

			now := time.Now()
			resp := GetSchedulerJobRESTResponse{
				Job: &SchedulerJobRESTResponse{
					ID:        123,
					Name:      "test-job",
					Status:    "running",
					Handler:   "backup",
					CreatedAt: now,
				},
				Timestamp: now,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{
			JobID: 123,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Job)
		assert.Equal(t, uint64(123), resp.Job.ID)
	})

	t.Run("job retrieval with tenant", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "tenant-abc", r.URL.Query().Get("tenant_id"))

			now := time.Now()
			resp := GetSchedulerJobRESTResponse{
				Job: &SchedulerJobRESTResponse{
					ID:        124,
					Name:      "tenant-job",
					Status:    "scheduled",
					CreatedAt: now,
				},
				Timestamp: now,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{
			JobID:    124,
			TenantID: "tenant-abc",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerGetJob(ctx, nil)
		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

// TestSchedulerUpdateJob tests the SchedulerUpdateJob client method.
func TestSchedulerUpdateJob(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful job update", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/api/v1/scheduler/jobs/123", r.URL.Path)

			now := time.Now()
			resp := UpdateSchedulerJobRESTResponse{
				Job: &SchedulerJobRESTResponse{
					ID:          123,
					Name:        "updated-job",
					Description: "Updated description",
					Status:      "scheduled",
					Handler:     "backup",
					CreatedAt:   now,
					UpdatedAt:   now,
				},
				Timestamp: now,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerUpdateJob(ctx, &service.UpdateSchedulerJobRequest{
			JobID:       123,
			Name:        "updated-job",
			Description: "Updated description",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "updated-job", resp.Job.Name)
	})

	t.Run("update with execute at", func(t *testing.T) {
		executeAt := time.Now().Add(2 * time.Hour)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req UpdateSchedulerJobRESTRequest
			json.NewDecoder(r.Body).Decode(&req)
			assert.NotEmpty(t, req.ExecuteAt)

			now := time.Now()
			resp := UpdateSchedulerJobRESTResponse{
				Job: &SchedulerJobRESTResponse{
					ID:        125,
					Name:      "rescheduled-job",
					Status:    "pending",
					CreatedAt: now,
				},
				Timestamp: now,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerUpdateJob(ctx, &service.UpdateSchedulerJobRequest{
			JobID:     125,
			Name:      "rescheduled-job",
			ExecuteAt: executeAt,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerUpdateJob(ctx, nil)
		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

// TestSchedulerDeleteJob tests the SchedulerDeleteJob client method.
func TestSchedulerDeleteJob(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful job deletion", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/v1/scheduler/jobs/123", r.URL.Path)

			resp := DeleteSchedulerJobRESTResponse{
				JobID:     123,
				Deleted:   true,
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerDeleteJob(ctx, &service.DeleteSchedulerJobRequest{
			JobID: 123,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Deleted)
		assert.Equal(t, uint64(123), resp.JobID)
	})

	t.Run("delete with force and tenant", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "true", r.URL.Query().Get("force"))
			assert.Equal(t, "tenant-xyz", r.URL.Query().Get("tenant_id"))

			resp := DeleteSchedulerJobRESTResponse{
				JobID:     126,
				Deleted:   true,
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerDeleteJob(ctx, &service.DeleteSchedulerJobRequest{
			JobID:    126,
			TenantID: "tenant-xyz",
			Force:    true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Deleted)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerDeleteJob(ctx, nil)
		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

// TestSchedulerListJobs tests the SchedulerListJobs client method.
func TestSchedulerListJobs(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful job listing", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v1/scheduler/jobs", r.URL.Path)

			now := time.Now()
			resp := ListSchedulerJobsRESTResponse{
				Jobs: []*SchedulerJobRESTResponse{
					{
						ID:        1,
						Name:      "job-1",
						Status:    "scheduled",
						Handler:   "backup",
						CreatedAt: now,
					},
					{
						ID:        2,
						Name:      "job-2",
						Status:    "running",
						Handler:   "cleanup",
						CreatedAt: now,
					},
				},
				TotalCount: 2,
				HasMore:    false,
				Timestamp:  now,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerListJobs(ctx, &service.ListSchedulerJobsRequest{})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Jobs, 2)
		assert.Equal(t, int(2), resp.TotalCount)
	})

	t.Run("list with filters", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "user", r.URL.Query().Get("owner_type"))
			assert.Equal(t, "user-123", r.URL.Query().Get("owner_id"))
			assert.Equal(t, "running", r.URL.Query().Get("status"))
			assert.Equal(t, "backup", r.URL.Query().Get("handler"))
			assert.Equal(t, "10", r.URL.Query().Get("limit"))

			resp := ListSchedulerJobsRESTResponse{
				Jobs:       []*SchedulerJobRESTResponse{},
				TotalCount: 0,
				Timestamp:  time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerListJobs(ctx, &service.ListSchedulerJobsRequest{
			OwnerType: "user",
			OwnerID:   "user-123",
			Status:    "running",
			Handler:   "backup",
			Limit:     10,
		})

		require.NoError(t, err)
	})

	t.Run("nil request uses defaults", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := ListSchedulerJobsRESTResponse{
				Jobs:      []*SchedulerJobRESTResponse{},
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerListJobs(ctx, nil)
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

// TestSchedulerTriggerJob tests the SchedulerTriggerJob client method.
func TestSchedulerTriggerJob(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful job trigger", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/trigger")

			resp := TriggerSchedulerJobRESTResponse{
				JobID:     123,
				Status:    "running",
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerTriggerJob(ctx, &service.TriggerSchedulerJobRequest{
			JobID: 123,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(123), resp.JobID)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerTriggerJob(ctx, nil)
		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

// TestSchedulerPauseJob tests the SchedulerPauseJob client method.
func TestSchedulerPauseJob(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful job pause", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/pause")

			resp := PauseSchedulerJobRESTResponse{
				JobID:     123,
				OldStatus: "scheduled",
				NewStatus: "paused",
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerPauseJob(ctx, &service.PauseSchedulerJobRequest{
			JobID: 123,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(123), resp.JobID)
		assert.Equal(t, service.SchedulerJobStatus("paused"), resp.NewStatus)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerPauseJob(ctx, nil)
		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

// TestSchedulerResumeJob tests the SchedulerResumeJob client method.
func TestSchedulerResumeJob(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful job resume", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/resume")

			resp := ResumeSchedulerJobRESTResponse{
				JobID:     123,
				OldStatus: "paused",
				NewStatus: "scheduled",
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerResumeJob(ctx, &service.ResumeSchedulerJobRequest{
			JobID: 123,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(123), resp.JobID)
		assert.Equal(t, service.SchedulerJobStatus("scheduled"), resp.NewStatus)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerResumeJob(ctx, nil)
		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

// TestSchedulerCancelJob tests the SchedulerCancelJob client method.
func TestSchedulerCancelJob(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful job cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/cancel")

			resp := CancelSchedulerJobRESTResponse{
				JobID:     123,
				OldStatus: "running",
				NewStatus: "cancelled",
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerCancelJob(ctx, &service.CancelSchedulerJobRequest{
			JobID: 123,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(123), resp.JobID)
		assert.Equal(t, service.SchedulerJobStatus("cancelled"), resp.NewStatus)
	})

	t.Run("cancel with reason", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req CancelSchedulerJobRESTRequest
			json.NewDecoder(r.Body).Decode(&req)
			assert.Equal(t, "user requested", req.Reason)

			resp := CancelSchedulerJobRESTResponse{
				JobID:     124,
				OldStatus: "scheduled",
				NewStatus: "cancelled",
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerCancelJob(ctx, &service.CancelSchedulerJobRequest{
			JobID:  124,
			Reason: "user requested",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerCancelJob(ctx, nil)
		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

// TestSchedulerGetJobHistory tests the SchedulerGetJobHistory client method.
func TestSchedulerGetJobHistory(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful history retrieval", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/history")

			now := time.Now()
			resp := GetSchedulerJobHistoryRESTResponse{
				Executions: []*SchedulerJobExecutionRESTResponse{
					{
						ID:        1,
						JobID:     123,
						Status:    "completed",
						StartedAt: now.Add(-time.Hour),
						EndedAt:   now.Add(-30 * time.Minute),
					},
					{
						ID:        2,
						JobID:     123,
						Status:    "failed",
						StartedAt: now.Add(-2 * time.Hour),
						Error:     "timeout exceeded",
					},
				},
				HasMore:   false,
				Timestamp: now,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerGetJobHistory(ctx, &service.GetSchedulerJobHistoryRequest{
			JobID: 123,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Executions, 2)
	})

	t.Run("history with pagination", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "5", r.URL.Query().Get("limit"))

			resp := GetSchedulerJobHistoryRESTResponse{
				Executions: []*SchedulerJobExecutionRESTResponse{},
				HasMore:    true,
				Timestamp:  time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerGetJobHistory(ctx, &service.GetSchedulerJobHistoryRequest{
			JobID: 123,
			Limit: 5,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.HasMore)
	})

	t.Run("nil request returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		_, err = client.SchedulerGetJobHistory(ctx, nil)
		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

// TestSchedulerGetStatus tests the SchedulerGetStatus client method.
func TestSchedulerGetStatus(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("successful status retrieval", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v1/scheduler/status", r.URL.Path)

			resp := GetSchedulerStatusRESTResponse{
				Running: true,
				Stats: &SchedulerStatsRESTResponse{
					JobCount:     100,
					RunningJobs:  25,
					SuccessCount: 70,
					FailureCount: 5,
				},
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerGetStatus(ctx)

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Stats)
		assert.True(t, resp.Running)
		assert.Equal(t, uint64(100), resp.Stats.JobCount)
		assert.Equal(t, uint64(25), resp.Stats.RunningJobs)
	})

	t.Run("status with nil stats", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := GetSchedulerStatusRESTResponse{
				Running:   false,
				Stats:     nil,
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SchedulerGetStatus(ctx)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Nil(t, resp.Stats)
	})
}
