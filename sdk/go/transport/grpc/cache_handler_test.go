// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"math"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cache"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeCacheServer records the cache RPCs it receives. A non-OK code makes
// every RPC fail with that code.
type fakeCacheServer struct {
	pb.UnimplementedQuicBaaSServer
	code codes.Code

	calls      int
	lastUpdate *pb.UpdateCacheConfigRequest
}

func (f *fakeCacheServer) fail() error {
	f.calls++
	if f.code != codes.OK {
		return status.Error(f.code, "refused")
	}
	return nil
}

func (f *fakeCacheServer) GetCacheStats(context.Context, *pb.GetCacheStatsRequest) (*pb.GetCacheStatsResponse, error) {
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.GetCacheStatsResponse{
		Enabled: true, L1Size: 40, L1Capacity: 100, L1Hits: 30, L1Misses: 10, L1HitRate: 0.75, L1Evictions: 5,
	}, nil
}

func (f *fakeCacheServer) ClearCache(context.Context, *pb.ClearCacheRequest) (*pb.ClearCacheResponse, error) {
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.ClearCacheResponse{Success: true, EntriesCleared: 12}, nil
}

func (f *fakeCacheServer) GetCacheConfig(context.Context, *pb.GetCacheConfigRequest) (*pb.GetCacheConfigResponse, error) {
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.GetCacheConfigResponse{Enabled: true, L1Size: 256, L1TtlSeconds: 60, CleanupIntervalSeconds: 30}, nil
}

func (f *fakeCacheServer) UpdateCacheConfig(_ context.Context, req *pb.UpdateCacheConfigRequest) (*pb.UpdateCacheConfigResponse, error) {
	f.lastUpdate = req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.UpdateCacheConfigResponse{Success: true, Message: "updated"}, nil
}

// newCacheClient serves fake on a bufconn listener and returns the real SDK
// cache client over the gRPC transport.
func newCacheClient(t *testing.T, fake *fakeCacheServer) (cache.Client, *Transport) {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterQuicBaaSServer(s, fake)
	})
	tr := &Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: srv.dial(t)}
	return cache.NewClient(tr), tr
}

func TestCacheHandler_GetCacheStatsMapsTheL1Counters(t *testing.T) {
	client, _ := newCacheClient(t, &fakeCacheServer{})

	resp, err := client.GetCacheStats(context.Background(), &cache.GetCacheStatsRequest{})
	if err != nil {
		t.Fatalf("GetCacheStats: %v", err)
	}
	want := &cache.GetCacheStatsResponse{HitRate: 0.75, Size: 40, MaxSize: 100, Evictions: 5, Hits: 30, Misses: 10}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("GetCacheStats = %+v, want %+v", resp, want)
	}
}

func TestCacheHandler_ClearCacheClearsAll(t *testing.T) {
	fake := &fakeCacheServer{}
	client, _ := newCacheClient(t, fake)

	resp, err := client.ClearCache(context.Background(), &cache.ClearCacheRequest{All: true})
	if err != nil {
		t.Fatalf("ClearCache: %v", err)
	}
	if resp.EntriesCleared != 12 || fake.calls != 1 {
		t.Errorf("ClearCache = %+v after %d calls, want 12 entries after 1", resp, fake.calls)
	}
}

func TestCacheHandler_GetCacheConfigMapsEveryField(t *testing.T) {
	client, _ := newCacheClient(t, &fakeCacheServer{})

	resp, err := client.GetCacheConfig(context.Background(), &cache.GetCacheConfigRequest{})
	if err != nil {
		t.Fatalf("GetCacheConfig: %v", err)
	}
	want := &cache.GetCacheConfigResponse{L1Size: 256, L1TTL: time.Minute, CleanupInterval: 30 * time.Second, Enabled: true}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("GetCacheConfig = %+v, want %+v", resp, want)
	}
}

func TestCacheHandler_UpdateCacheConfigCarriesSizeAndTTL(t *testing.T) {
	fake := &fakeCacheServer{}
	client, _ := newCacheClient(t, fake)
	size, ttl := 512, 90*time.Second

	resp, err := client.UpdateCacheConfig(context.Background(), &cache.UpdateCacheConfigRequest{L1Size: &size, L1TTL: &ttl})
	if err != nil {
		t.Fatalf("UpdateCacheConfig: %v", err)
	}
	if fake.lastUpdate.GetL1Size() != 512 || fake.lastUpdate.GetL1TtlSeconds() != 90 {
		t.Errorf("UpdateCacheConfig sent %+v, want l1_size 512 and l1_ttl_seconds 90", fake.lastUpdate)
	}
	if !resp.Updated {
		t.Errorf("UpdateCacheConfig = %+v, want Updated", resp)
	}
}

// The gRPC cache RPCs carry no entity-type filter and no cleanup interval,
// and read a zero size or TTL as "no change". The handler refuses each such
// request rather than letting the server act on something else: ClearCache
// on the wire clears every entry.
func TestCacheHandler_RefusesRequestsTheWireCannotCarry(t *testing.T) {
	fake := &fakeCacheServer{}
	client, _ := newCacheClient(t, fake)
	ctx := context.Background()
	zero, huge := 0, math.MaxInt32+1
	subSecond, interval := 1500*time.Millisecond, time.Minute

	unimplemented := map[string]func() error{
		"GetCacheStats by entity type": func() error {
			_, err := client.GetCacheStats(ctx, &cache.GetCacheStatsRequest{EntityType: "user"})
			return err
		},
		"ClearCache by entity type": func() error {
			_, err := client.ClearCache(ctx, &cache.ClearCacheRequest{EntityType: "user"})
			return err
		},
		"UpdateCacheConfig cleanup interval": func() error {
			_, err := client.UpdateCacheConfig(ctx, &cache.UpdateCacheConfigRequest{CleanupInterval: &interval})
			return err
		},
	}
	for name, call := range unimplemented {
		if err := call(); !errors.IsUnimplemented(err) {
			t.Errorf("%s gave %v, want unimplemented", name, err)
		}
	}

	invalid := map[string]*cache.UpdateCacheConfigRequest{
		"zero size":        {L1Size: &zero},
		"oversized size":   {L1Size: &huge},
		"sub-second TTL":   {L1TTL: &subSecond},
		"zero TTL":         {L1TTL: new(time.Duration)},
		"size and bad TTL": {L1Size: &huge, L1TTL: &subSecond},
	}
	for name, req := range invalid {
		if _, err := client.UpdateCacheConfig(ctx, req); !errors.IsInvalidInput(err) {
			t.Errorf("UpdateCacheConfig with a %s gave %v, want invalid input", name, err)
		}
	}
	if fake.calls != 0 {
		t.Errorf("a refused request reached the server %d times", fake.calls)
	}
}

func TestCacheHandler_MapsServerRefusals(t *testing.T) {
	codesToPredicates := map[codes.Code]func(error) bool{
		codes.InvalidArgument:  errors.IsInvalidInput,
		codes.NotFound:         errors.IsNotFound,
		codes.PermissionDenied: errors.IsForbidden,
	}
	size := 1
	for code, is := range codesToPredicates {
		client, _ := newCacheClient(t, &fakeCacheServer{code: code})
		ctx := context.Background()
		calls := map[string]func() error{
			"GetCacheStats": func() error {
				_, err := client.GetCacheStats(ctx, &cache.GetCacheStatsRequest{})
				return err
			},
			"ClearCache": func() error {
				_, err := client.ClearCache(ctx, &cache.ClearCacheRequest{All: true})
				return err
			},
			"GetCacheConfig": func() error {
				_, err := client.GetCacheConfig(ctx, &cache.GetCacheConfigRequest{})
				return err
			},
			"UpdateCacheConfig": func() error {
				_, err := client.UpdateCacheConfig(ctx, &cache.UpdateCacheConfigRequest{L1Size: &size})
				return err
			},
		}
		for verb, call := range calls {
			if err := call(); !is(err) {
				t.Errorf("%s answering %s gave %v", verb, code, err)
			}
		}
	}
}

func TestCacheHandler_RefusesUnknownMethodsAndRequestTypes(t *testing.T) {
	fake := &fakeCacheServer{}
	_, tr := newCacheClient(t, fake)
	ctx := context.Background()

	if _, err := tr.Request(ctx, "quicbaas.Cache", "Warm", &cache.GetCacheStatsRequest{}); !errors.IsUnimplemented(err) {
		t.Errorf("an unknown cache method gave %v, want unimplemented", err)
	}
	if _, err := tr.Request(ctx, "quicbaas.Cache", "ClearCache", &cache.GetCacheStatsRequest{}); !errors.IsInvalidInput(err) {
		t.Errorf("a mistyped ClearCache request gave %v, want invalid input", err)
	}
	if fake.calls != 0 {
		t.Errorf("a refused request reached the server %d times", fake.calls)
	}
}

func TestCacheHandler_CarriesEveryVerb(t *testing.T) {
	iface := reflect.TypeOf((*cache.Client)(nil)).Elem()
	declared := make(map[string]bool, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		verb := iface.Method(i).Name
		declared[verb] = true
		if _, ok := cacheMethodMap[verb]; !ok {
			t.Errorf("the gRPC transport does not route cache.%s", verb)
		}
	}
	for method := range cacheMethodMap {
		if !declared[method] {
			t.Errorf("the gRPC transport routes %q, which the SDK interface does not declare", method)
		}
	}
	for _, name := range []string{"quicbaas.Cache", "Cache"} {
		if !isCacheService(name) {
			t.Errorf("isCacheService(%q) = false", name)
		}
	}
	if isCacheService("quicbaas.Backup") {
		t.Error("isCacheService claims the Backup service")
	}
}
