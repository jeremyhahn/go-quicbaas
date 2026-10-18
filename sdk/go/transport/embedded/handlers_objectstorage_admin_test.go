// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package embedded

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	osadmin "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
)

// objectStorageAdminDouble records the service requests the embedded
// handlers send and answers with fixed replies.
type objectStorageAdminDouble struct {
	service.ObjectStorageAdminService
	created *service.CreateBackendRequest
}

func (d *objectStorageAdminDouble) CreateBackend(_ context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
	d.created = req
	return &service.CreateBackendResponse{Name: req.Name, CreatedAt: 7}, nil
}

func (d *objectStorageAdminDouble) GetStorageStats(_ context.Context, _ *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
	return &service.GetStorageStatsResponse{
		TotalBackends: 1,
		BackendStats:  []*service.BackendInfo{{Name: "b1", Type: "s3", Tier: "cold", TotalSize: 42}},
	}, nil
}

type quicBaaSWithObjectStorageAdmin struct {
	*mockQuicBaaS
	admin service.ObjectStorageAdminService
}

func (q *quicBaaSWithObjectStorageAdmin) ObjectStorageAdminService() service.ObjectStorageAdminService {
	return q.admin
}

func newObjectStorageAdminClient(t *testing.T, admin service.ObjectStorageAdminService) osadmin.Client {
	t.Helper()
	var svc service.QuicBaaS = &mockQuicBaaS{}
	if admin != nil {
		svc = &quicBaaSWithObjectStorageAdmin{mockQuicBaaS: &mockQuicBaaS{}, admin: admin}
	}
	tr, err := New(svc)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return osadmin.NewClient(tr)
}

func TestObjectStorageAdminCreateBackendConvertsEnums(t *testing.T) {
	double := &objectStorageAdminDouble{}
	client := newObjectStorageAdminClient(t, double)

	resp, err := client.CreateBackend(context.Background(), &osadmin.CreateBackendRequest{
		Name: "b1", Type: osadmin.BackendTypeS3, Tier: osadmin.StorageTierHot, Enabled: true,
		Config: map[string]string{"bucket": "b1"},
	})
	if err != nil {
		t.Fatalf("CreateBackend: %v", err)
	}
	if resp.Name != "b1" || resp.CreatedAt != 7 {
		t.Fatalf("response = %+v", resp)
	}
	if double.created.Type != service.BackendTypeS3 || double.created.Tier != service.StorageTierHot {
		t.Fatalf("service request = %+v", double.created)
	}
}

func TestObjectStorageAdminGetStorageStatsDecodesBackends(t *testing.T) {
	client := newObjectStorageAdminClient(t, &objectStorageAdminDouble{})

	resp, err := client.GetStorageStats(context.Background(), &osadmin.GetStorageStatsRequest{IncludeBackends: true})
	if err != nil {
		t.Fatalf("GetStorageStats: %v", err)
	}
	if len(resp.BackendStats) != 1 {
		t.Fatalf("backend stats = %+v", resp.BackendStats)
	}
	b := resp.BackendStats[0]
	if b.Type != osadmin.BackendTypeS3 || b.Tier != osadmin.StorageTierCold || b.UsedBytes != 42 {
		t.Fatalf("backend = %+v", b)
	}
}

func TestObjectStorageAdminNotConfigured(t *testing.T) {
	client := newObjectStorageAdminClient(t, nil)

	if _, err := client.CompactBackend(context.Background(), &osadmin.CompactBackendRequest{Name: "b1"}); err == nil {
		t.Fatal("CompactBackend without a service: want error")
	}
}

func TestObjectStorageAdminWrongRequestType(t *testing.T) {
	tr, err := New(&quicBaaSWithObjectStorageAdmin{mockQuicBaaS: &mockQuicBaaS{}, admin: &objectStorageAdminDouble{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := tr.Request(context.Background(), objectStorageAdminServiceName, "RepairObject", &osadmin.CompactBackendRequest{}); err == nil {
		t.Fatal("RepairObject with a CompactBackendRequest: want error")
	}
}
