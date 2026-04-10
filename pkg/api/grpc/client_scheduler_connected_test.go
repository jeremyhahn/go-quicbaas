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

package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const schedulerBufSize = 1024 * 1024

// =============================================================================
// Minimal Mock SchedulerService Server
// =============================================================================

type minimalSchedulerServer struct {
	pb.UnimplementedSchedulerServiceServer
}

func (m *minimalSchedulerServer) CreateJob(_ context.Context, req *pb.CreateJobRequest) (*pb.CreateJobResponse, error) {
	return &pb.CreateJobResponse{
		Job: &pb.Job{
			Id:       1,
			Name:     req.Name,
			Status:   pb.JobStatus_JOB_STATUS_PENDING,
			Schedule: req.Schedule,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) GetJob(_ context.Context, req *pb.GetJobRequest) (*pb.GetJobResponse, error) {
	return &pb.GetJobResponse{
		Job: &pb.Job{
			Id:     req.JobId,
			Name:   "test-job",
			Status: pb.JobStatus_JOB_STATUS_PENDING,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) UpdateJob(_ context.Context, req *pb.UpdateJobRequest) (*pb.UpdateJobResponse, error) {
	return &pb.UpdateJobResponse{
		Job: &pb.Job{
			Id:     req.JobId,
			Name:   "updated-job",
			Status: pb.JobStatus_JOB_STATUS_PENDING,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) DeleteJob(_ context.Context, req *pb.DeleteJobRequest) (*pb.DeleteJobResponse, error) {
	return &pb.DeleteJobResponse{
		JobId:     req.JobId,
		Deleted:   true,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) ListJobs(_ context.Context, _ *pb.ListJobsRequest) (*pb.ListJobsResponse, error) {
	return &pb.ListJobsResponse{
		Jobs:       []*pb.Job{},
		TotalCount: 0,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) TriggerJob(_ context.Context, req *pb.TriggerJobRequest) (*pb.TriggerJobResponse, error) {
	return &pb.TriggerJobResponse{
		JobId:     req.JobId,
		Status:    pb.JobStatus_JOB_STATUS_RUNNING,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) PauseJob(_ context.Context, req *pb.PauseJobRequest) (*pb.PauseJobResponse, error) {
	return &pb.PauseJobResponse{
		JobId:     req.JobId,
		OldStatus: pb.JobStatus_JOB_STATUS_PENDING,
		NewStatus: pb.JobStatus_JOB_STATUS_PAUSED,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) ResumeJob(_ context.Context, req *pb.ResumeJobRequest) (*pb.ResumeJobResponse, error) {
	return &pb.ResumeJobResponse{
		JobId:     req.JobId,
		OldStatus: pb.JobStatus_JOB_STATUS_PAUSED,
		NewStatus: pb.JobStatus_JOB_STATUS_PENDING,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) CancelJob(_ context.Context, req *pb.CancelJobRequest) (*pb.CancelJobResponse, error) {
	return &pb.CancelJobResponse{
		JobId:     req.JobId,
		OldStatus: pb.JobStatus_JOB_STATUS_PENDING,
		NewStatus: pb.JobStatus_JOB_STATUS_CANCELED,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) GetJobHistory(_ context.Context, _ *pb.GetJobHistoryRequest) (*pb.GetJobHistoryResponse, error) {
	return &pb.GetJobHistoryResponse{
		Executions: []*pb.JobExecution{},
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *minimalSchedulerServer) GetSchedulerStatus(_ context.Context, _ *pb.GetSchedulerStatusRequest) (*pb.GetSchedulerStatusResponse, error) {
	return &pb.GetSchedulerStatusResponse{
		Running:   true,
		Timestamp: time.Now().Unix(),
	}, nil
}

// =============================================================================
// Test Server Setup
// =============================================================================

func setupSchedulerTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener) {
	t.Helper()
	lis := bufconn.Listen(schedulerBufSize)
	server := grpc.NewServer()
	pb.RegisterSchedulerServiceServer(server, &minimalSchedulerServer{})
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

func setupConnectedSchedulerClient(t *testing.T) (*Client, *grpc.Server) {
	t.Helper()
	server, lis := setupSchedulerTestServer(t)
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := &Client{
		conn: conn,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      1,
			retryBackoff:    10 * time.Millisecond,
			maxRetryBackoff: 100 * time.Millisecond,
		},
		connected: true,
	}

	return client, server
}

// =============================================================================
// Scheduler Client Connected Tests
// =============================================================================

func TestClientSchedulerCreateJob_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerCreateJob(context.Background(), &service.CreateSchedulerJobRequest{
		Name:     "test-job",
		Schedule: "*/5 * * * *",
		Handler:  "cleanup",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Job)
	assert.Equal(t, uint64(1), resp.Job.ID)
	assert.Equal(t, "test-job", resp.Job.Name)
}

func TestClientSchedulerGetJob_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerGetJob(context.Background(), &service.GetSchedulerJobRequest{
		JobID: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Job)
	assert.Equal(t, uint64(1), resp.Job.ID)
}

func TestClientSchedulerUpdateJob_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerUpdateJob(context.Background(), &service.UpdateSchedulerJobRequest{
		JobID:   1,
		Name:    "updated-job",
		Handler: "new-handler",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Job)
}

func TestClientSchedulerDeleteJob_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerDeleteJob(context.Background(), &service.DeleteSchedulerJobRequest{
		JobID: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
}

func TestClientSchedulerListJobs_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerListJobs(context.Background(), &service.ListSchedulerJobsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientSchedulerTriggerJob_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerTriggerJob(context.Background(), &service.TriggerSchedulerJobRequest{
		JobID: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(1), resp.JobID)
}

func TestClientSchedulerPauseJob_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerPauseJob(context.Background(), &service.PauseSchedulerJobRequest{
		JobID: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(1), resp.JobID)
}

func TestClientSchedulerResumeJob_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerResumeJob(context.Background(), &service.ResumeSchedulerJobRequest{
		JobID: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(1), resp.JobID)
}

func TestClientSchedulerCancelJob_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerCancelJob(context.Background(), &service.CancelSchedulerJobRequest{
		JobID: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(1), resp.JobID)
}

func TestClientSchedulerGetJobHistory_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerGetJobHistory(context.Background(), &service.GetSchedulerJobHistoryRequest{
		JobID: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientSchedulerGetStatus_Connected(t *testing.T) {
	client, server := setupConnectedSchedulerClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.SchedulerGetStatus(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Running)
}
