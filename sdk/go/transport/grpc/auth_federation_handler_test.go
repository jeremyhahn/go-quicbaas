// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package grpc

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

var (
	authFedCreated = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	authFedUpdated = time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	authFedActive  = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	authFedExpires = time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
)

// authFedProvider is the provider every fake federation answer carries.
func authFedProvider() *iampb.ExternalProvider {
	return &iampb.ExternalProvider{
		Id:            "prov-1",
		TenantId:      "t-1",
		Name:          "okta",
		Type:          iampb.ProviderType_PROVIDER_TYPE_OIDC,
		Enabled:       true,
		ClientId:      "client-1",
		IssuerUrl:     "https://issuer.example",
		AuthUrl:       "https://issuer.example/auth",
		TokenUrl:      "https://issuer.example/token",
		UserinfoUrl:   "https://issuer.example/userinfo",
		Scopes:        []string{"openid", "email"},
		AutoProvision: true,
		AllowLinking:  true,
		DefaultRoles:  []string{"viewer"},
		CreatedAt:     timestamppb.New(authFedCreated),
		UpdatedAt:     timestamppb.New(authFedUpdated),
	}
}

// wantAuthFedProvider is authFedProvider as the SDK reports it.
func wantAuthFedProvider() service.AuthProvider {
	return service.AuthProvider{
		ID:            "prov-1",
		TenantID:      "t-1",
		Name:          "okta",
		Type:          service.ProviderTypeOIDC,
		Enabled:       true,
		IssuerURL:     "https://issuer.example",
		AuthURL:       "https://issuer.example/auth",
		TokenURL:      "https://issuer.example/token",
		UserInfoURL:   "https://issuer.example/userinfo",
		Scopes:        []string{"openid", "email"},
		AutoProvision: true,
		AllowLinking:  true,
		DefaultRoles:  []string{"viewer"},
		CreatedAt:     authFedCreated,
		UpdatedAt:     authFedUpdated,
	}
}

// authFedSession is the session every fake session answer carries.
func authFedSession() *iampb.Session {
	return &iampb.Session{
		Id:          "sess-1",
		UserId:      "10001",
		TenantId:    "t-1",
		CreatedAt:   timestamppb.New(authFedCreated),
		LastActive:  timestamppb.New(authFedActive),
		ExpiresAt:   timestamppb.New(authFedExpires),
		MfaVerified: true,
		IpAddress:   "192.0.2.10",
		UserAgent:   "agent/1.0",
		DeviceInfo:  "laptop",
	}
}

// fakeAuthFederationServer records each FederationService request it is
// sent and answers with fixed values, or with fail when it is set.
type fakeAuthFederationServer struct {
	iampb.UnimplementedFederationServiceServer
	fail     error
	identity *iampb.FederatedIdentity
	received map[string]proto.Message
	// providers is served by ListProviders one limit/offset page at a time.
	providers []*iampb.ExternalProvider
	// failFromOffset, when positive, fails every ListProviders page that
	// starts at or past it with fail.
	failFromOffset int32
	// listOffsets records the offset of every ListProviders page requested.
	listOffsets []int32
}

func newFakeAuthFederationServer() *fakeAuthFederationServer {
	return &fakeAuthFederationServer{
		received:  map[string]proto.Message{},
		identity:  &iampb.FederatedIdentity{ProviderId: "prov-1", TenantId: "t-1"},
		providers: []*iampb.ExternalProvider{authFedProvider()},
	}
}

func (f *fakeAuthFederationServer) GetProvider(_ context.Context, req *iampb.GetProviderRequest) (*iampb.ExternalProvider, error) {
	f.received["GetProvider"] = req
	if f.fail != nil {
		return nil, f.fail
	}
	return authFedProvider(), nil
}

func (f *fakeAuthFederationServer) ListProviders(_ context.Context, req *iampb.ListProvidersRequest) (*iampb.ListProvidersResponse, error) {
	f.received["ListProviders"] = req
	f.listOffsets = append(f.listOffsets, req.GetOffset())
	if f.fail != nil && (f.failFromOffset == 0 || req.GetOffset() >= f.failFromOffset) {
		return nil, f.fail
	}
	total := int32(len(f.providers))
	start := min(req.GetOffset(), total)
	end := min(start+req.GetLimit(), total)
	return &iampb.ListProvidersResponse{Providers: f.providers[start:end], Total: total}, nil
}

func (f *fakeAuthFederationServer) BeginFederatedLogin(_ context.Context, req *iampb.BeginFederatedLoginRequest) (*iampb.BeginFederatedLoginResponse, error) {
	f.received["BeginFederatedLogin"] = req
	if f.fail != nil {
		return nil, f.fail
	}
	return &iampb.BeginFederatedLoginResponse{
		AuthUrl:   "https://issuer.example/auth?state=s-1",
		State:     "s-1",
		ExpiresAt: timestamppb.New(authFedExpires),
	}, nil
}

func (f *fakeAuthFederationServer) HandleFederatedCallback(_ context.Context, req *iampb.HandleFederatedCallbackRequest) (*iampb.HandleFederatedCallbackResponse, error) {
	f.received["HandleFederatedCallback"] = req
	if f.fail != nil {
		return nil, f.fail
	}
	return &iampb.HandleFederatedCallbackResponse{
		User:              &iampb.User{Id: "10001", TenantId: "t-1", Email: "ada@example.com", Name: "Ada Lovelace", Username: "ada"},
		FederatedIdentity: f.identity,
		AccessToken:       "access-1",
		RefreshToken:      "refresh-1",
		ExpiresIn:         3600,
		Provisioned:       true,
	}, nil
}

// fakeAuthSessionServer records each SessionService request it is sent.
type fakeAuthSessionServer struct {
	iampb.UnimplementedSessionServiceServer
	fail     error
	received map[string]proto.Message
}

func (f *fakeAuthSessionServer) GetSession(_ context.Context, req *iampb.GetSessionRequest) (*iampb.Session, error) {
	f.received["GetSession"] = req
	if f.fail != nil {
		return nil, f.fail
	}
	return authFedSession(), nil
}

func (f *fakeAuthSessionServer) RevokeSession(_ context.Context, req *iampb.RevokeSessionRequest) (*iampb.RevokeSessionResponse, error) {
	f.received["RevokeSession"] = req
	if f.fail != nil {
		return nil, f.fail
	}
	return &iampb.RevokeSessionResponse{Success: true}, nil
}

// newAuthFederationClient drives the real auth SDK client over bufconn
// against the fake federation and session servers.
func newAuthFederationClient(t *testing.T, fed *fakeAuthFederationServer, sess *fakeAuthSessionServer) auth.Client {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterFederationServiceServer(s, fed)
		iampb.RegisterSessionServiceServer(s, sess)
	})
	return auth.NewClient(&Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: srv.dial(t)})
}

func requireReceived(t *testing.T, received map[string]proto.Message, rpc string, want proto.Message) {
	t.Helper()
	got, ok := received[rpc]
	if !ok {
		t.Fatalf("%s was not called", rpc)
	}
	if !proto.Equal(got, want) {
		t.Fatalf("%s request = %v, want %v", rpc, got, want)
	}
}

func TestGRPCAuth_FederationVerbsReachTheServer(t *testing.T) {
	fed := newFakeAuthFederationServer()
	sess := &fakeAuthSessionServer{received: map[string]proto.Message{}}
	client := newAuthFederationClient(t, fed, sess)
	ctx := context.Background()

	begin, err := client.BeginFederatedLogin(ctx, &service.BeginFederatedLoginRequest{
		TenantID: "t-1", ProviderID: "prov-1", RedirectURI: "https://app.example/cb",
	})
	if err != nil {
		t.Fatalf("BeginFederatedLogin: %v", err)
	}
	if want := (service.BeginFederatedLoginResponse{AuthorizationURL: "https://issuer.example/auth?state=s-1", State: "s-1"}); *begin != want {
		t.Fatalf("BeginFederatedLogin = %+v, want %+v", *begin, want)
	}
	requireReceived(t, fed.received, "BeginFederatedLogin", &iampb.BeginFederatedLoginRequest{
		TenantId: "t-1", ProviderId: "prov-1", RedirectUri: "https://app.example/cb",
	})

	before := time.Now()
	callback, err := client.HandleFederatedCallback(ctx, &service.HandleFederatedCallbackRequest{
		TenantID: "t-1", ProviderID: "prov-1", Code: "code-1", State: "s-1",
	})
	if err != nil {
		t.Fatalf("HandleFederatedCallback: %v", err)
	}
	requireReceived(t, fed.received, "HandleFederatedCallback", &iampb.HandleFederatedCallbackRequest{State: "s-1", Code: "code-1"})
	if callback.ExpiresAt.Before(before.Add(time.Hour)) || callback.ExpiresAt.After(time.Now().Add(time.Hour)) {
		t.Fatalf("ExpiresAt = %v, want one hour from the call", callback.ExpiresAt)
	}
	callback.ExpiresAt = time.Time{}
	want := service.HandleFederatedCallbackResponse{
		AccessToken: "access-1", RefreshToken: "refresh-1", TokenType: "Bearer",
		UserID: "10001", Username: "ada", Email: "ada@example.com", NewUser: true,
	}
	if *callback != want {
		t.Fatalf("HandleFederatedCallback = %+v, want %+v", *callback, want)
	}

	providers, err := client.ListProviders(ctx, "t-1")
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	requireReceived(t, fed.received, "ListProviders", &iampb.ListProvidersRequest{TenantId: "t-1", Limit: 100, Offset: 1})
	if !reflect.DeepEqual(providers.Providers, []service.AuthProvider{wantAuthFedProvider()}) {
		t.Fatalf("ListProviders = %+v", providers.Providers)
	}

	provider, err := client.GetProvider(ctx, "t-1", "prov-1")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if !reflect.DeepEqual(*provider, wantAuthFedProvider()) {
		t.Fatalf("GetProvider = %+v", *provider)
	}
}

func TestGRPCAuth_ProviderVerbsTakeTheTenantFromContext(t *testing.T) {
	fed := newFakeAuthFederationServer()
	client := newAuthFederationClient(t, fed, &fakeAuthSessionServer{received: map[string]proto.Message{}})
	ctx := transport.ContextWithTenant(context.Background(), "t-ctx")

	if _, err := client.ListProviders(ctx, ""); err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	requireReceived(t, fed.received, "ListProviders", &iampb.ListProvidersRequest{TenantId: "t-ctx", Limit: 100, Offset: 1})
	if _, err := client.GetProvider(ctx, "", "prov-1"); err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	requireReceived(t, fed.received, "GetProvider", &iampb.GetProviderRequest{TenantId: "t-ctx", ProviderId: "prov-1"})
	if _, err := client.BeginFederatedLogin(ctx, &service.BeginFederatedLoginRequest{
		ProviderID: "prov-1", RedirectURI: "https://app.example/cb",
	}); err != nil {
		t.Fatalf("BeginFederatedLogin: %v", err)
	}
	requireReceived(t, fed.received, "BeginFederatedLogin", &iampb.BeginFederatedLoginRequest{
		TenantId: "t-ctx", ProviderId: "prov-1", RedirectUri: "https://app.example/cb",
	})
}

// Begin-login is public: an anonymous caller cannot pass the authorized
// GetProvider, so the SDK sends the provider ID and the server resolves it.
func TestGRPCAuth_BeginFederatedLoginSendsProviderIDWithoutGetProvider(t *testing.T) {
	fed := newFakeAuthFederationServer()
	client := newAuthFederationClient(t, fed, &fakeAuthSessionServer{received: map[string]proto.Message{}})

	if _, err := client.BeginFederatedLogin(context.Background(), &service.BeginFederatedLoginRequest{
		TenantID: "t-1", ProviderID: "prov-1", RedirectURI: "https://app.example/cb",
	}); err != nil {
		t.Fatalf("BeginFederatedLogin: %v", err)
	}
	if _, called := fed.received["GetProvider"]; called {
		t.Fatal("BeginFederatedLogin called GetProvider, which an anonymous caller is refused")
	}
	requireReceived(t, fed.received, "BeginFederatedLogin", &iampb.BeginFederatedLoginRequest{
		TenantId: "t-1", ProviderId: "prov-1", RedirectUri: "https://app.example/cb",
	})
}

func TestGRPCAuth_SessionVerbsReachTheServer(t *testing.T) {
	sess := &fakeAuthSessionServer{received: map[string]proto.Message{}}
	client := newAuthFederationClient(t, newFakeAuthFederationServer(), sess)
	ctx := context.Background()

	got, err := client.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	requireReceived(t, sess.received, "GetSession", &iampb.GetSessionRequest{SessionId: "sess-1"})
	want := service.AuthSession{
		ID: "sess-1", UserID: "10001", TenantID: "t-1",
		CreatedAt: authFedCreated, LastActive: authFedActive, ExpiresAt: authFedExpires,
		MFAVerified: true, IPAddress: "192.0.2.10", UserAgent: "agent/1.0", DeviceInfo: "laptop",
	}
	if *got != want {
		t.Fatalf("GetSession = %+v, want %+v", *got, want)
	}

	if err := client.RevokeSession(ctx, "sess-1"); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	requireReceived(t, sess.received, "RevokeSession", &iampb.RevokeSessionRequest{SessionId: "sess-1"})
}

func TestGRPCAuth_ServerRefusalsAreConverted(t *testing.T) {
	refusal := status.Error(codes.NotFound, "missing")
	fed := newFakeAuthFederationServer()
	fed.fail = refusal
	sess := &fakeAuthSessionServer{received: map[string]proto.Message{}, fail: refusal}
	client := newAuthFederationClient(t, fed, sess)
	ctx := context.Background()

	calls := map[string]func() error{
		"BeginFederatedLogin": func() error {
			_, err := client.BeginFederatedLogin(ctx, &service.BeginFederatedLoginRequest{
				TenantID: "t-1", ProviderID: "prov-1", RedirectURI: "https://app.example/cb",
			})
			return err
		},
		"HandleFederatedCallback": func() error {
			_, err := client.HandleFederatedCallback(ctx, &service.HandleFederatedCallbackRequest{
				ProviderID: "prov-1", Code: "code-1", State: "s-1",
			})
			return err
		},
		"ListProviders": func() error { _, err := client.ListProviders(ctx, "t-1"); return err },
		"GetProvider":   func() error { _, err := client.GetProvider(ctx, "t-1", "prov-1"); return err },
		"GetSession":    func() error { _, err := client.GetSession(ctx, "sess-1"); return err },
		"RevokeSession": func() error { return client.RevokeSession(ctx, "sess-1") },
	}
	for verb, call := range calls {
		if err := call(); !sdkerrors.IsNotFound(err) {
			t.Fatalf("%s = %v, want not found", verb, err)
		}
	}
}

// authFedProviders returns n providers with distinct IDs, in order.
func authFedProviders(n int) []*iampb.ExternalProvider {
	providers := make([]*iampb.ExternalProvider, n)
	for i := range providers {
		p := authFedProvider()
		p.Id = "prov-" + strconv.Itoa(i)
		providers[i] = p
	}
	return providers
}

func TestGRPCAuth_ListProvidersFetchesEveryPage(t *testing.T) {
	fed := newFakeAuthFederationServer()
	fed.providers = authFedProviders(250)
	client := newAuthFederationClient(t, fed, &fakeAuthSessionServer{received: map[string]proto.Message{}})

	resp, err := client.ListProviders(context.Background(), "t-1")
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(resp.Providers) != 250 {
		t.Fatalf("ListProviders returned %d providers, want 250", len(resp.Providers))
	}
	for i, p := range resp.Providers {
		if want := "prov-" + strconv.Itoa(i); p.ID != want {
			t.Fatalf("provider %d = %q, want %q", i, p.ID, want)
		}
	}
	if want := []int32{0, 100, 200, 250}; !reflect.DeepEqual(fed.listOffsets, want) {
		t.Fatalf("page offsets = %v, want %v", fed.listOffsets, want)
	}
}

func TestGRPCAuth_ListProvidersRefusesAPartialList(t *testing.T) {
	fed := newFakeAuthFederationServer()
	fed.providers = authFedProviders(150)
	fed.fail = status.Error(codes.Unavailable, "shard moved")
	fed.failFromOffset = 100
	client := newAuthFederationClient(t, fed, &fakeAuthSessionServer{received: map[string]proto.Message{}})

	resp, err := client.ListProviders(context.Background(), "t-1")
	if !sdkerrors.IsUnavailable(err) {
		t.Fatalf("ListProviders error = %v, want unavailable", err)
	}
	if resp != nil {
		t.Fatalf("ListProviders = %+v, want no partial list", resp)
	}
}

func TestGRPCAuth_CallbackRefusesAnotherProviderOrTenant(t *testing.T) {
	fed := newFakeAuthFederationServer()
	client := newAuthFederationClient(t, fed, &fakeAuthSessionServer{received: map[string]proto.Message{}})
	ctx := context.Background()

	requests := map[string]*service.HandleFederatedCallbackRequest{
		"provider": {ProviderID: "prov-2", Code: "code-1", State: "s-1"},
		"tenant":   {TenantID: "t-2", ProviderID: "prov-1", Code: "code-1", State: "s-1"},
	}
	for name, req := range requests {
		resp, err := client.HandleFederatedCallback(ctx, req)
		if !sdkerrors.IsInvalidInput(err) || resp != nil {
			t.Fatalf("%s mismatch = %+v, %v; want invalid input", name, resp, err)
		}
		if strings.Contains(err.Error(), "access-1") || strings.Contains(err.Error(), "refresh-1") {
			t.Fatalf("%s mismatch error carries a token: %v", name, err)
		}
	}
}

func TestGRPCAuth_FieldsWithNoProtoFieldAreRefused(t *testing.T) {
	fed := newFakeAuthFederationServer()
	client := newAuthFederationClient(t, fed, &fakeAuthSessionServer{received: map[string]proto.Message{}})
	ctx := context.Background()

	begins := map[string]*service.BeginFederatedLoginRequest{
		"State":     {ProviderID: "prov-1", RedirectURI: "https://app.example/cb", State: "secret-state"},
		"Scopes":    {ProviderID: "prov-1", RedirectURI: "https://app.example/cb", Scopes: []string{"groups"}},
		"Prompt":    {ProviderID: "prov-1", RedirectURI: "https://app.example/cb", Prompt: "login"},
		"LoginHint": {ProviderID: "prov-1", RedirectURI: "https://app.example/cb", LoginHint: "ada"},
	}
	for field, req := range begins {
		_, err := client.BeginFederatedLogin(ctx, req)
		if sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
			t.Fatalf("BeginFederatedLogin with %s = %v, want unimplemented", field, err)
		}
		if strings.Contains(err.Error(), "secret-state") {
			t.Fatalf("refusal carries the state value: %v", err)
		}
	}
	_, err := client.HandleFederatedCallback(ctx, &service.HandleFederatedCallbackRequest{
		ProviderID: "prov-1", Code: "code-1", State: "s-1", RedirectURI: "https://app.example/cb",
	})
	if sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("HandleFederatedCallback with RedirectURI = %v, want unimplemented", err)
	}
	if len(fed.received) != 0 {
		t.Fatalf("a refused request reached the server: %v", fed.received)
	}
}

func TestGRPCAuth_VerbsWithNoRPCAreRefused(t *testing.T) {
	sess := &fakeAuthSessionServer{received: map[string]proto.Message{}}
	client := newAuthFederationClient(t, newFakeAuthFederationServer(), sess)
	ctx := context.Background()

	calls := map[string]func() error{
		"GetJWKS":                func() error { _, err := client.GetJWKS(ctx); return err },
		"GetOpenIDConfiguration": func() error { _, err := client.GetOpenIDConfiguration(ctx); return err },
		"ListSessions":           func() error { _, err := client.ListSessions(ctx); return err },
		"RevokeAllSessions":      func() error { return client.RevokeAllSessions(ctx) },
	}
	for verb, call := range calls {
		if err := call(); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
			t.Fatalf("%s = %v, want unimplemented", verb, err)
		}
	}
	if len(sess.received) != 0 {
		t.Fatalf("a refused verb reached the server: %v", sess.received)
	}
	if _, err := newAuthHandler(nil).handleRequest(ctx, "quicbaas.Auth", "Nope", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("unknown method = %v, want unimplemented", err)
	}
}

func TestGRPCAuth_FederationVerbsRefuseTheWrongRequestType(t *testing.T) {
	h := newAuthHandler(nil)
	for _, verb := range []string{"BeginFederatedLogin", "HandleFederatedCallback", "ListProviders", "GetProvider", "GetSession", "RevokeSession"} {
		if _, err := h.handleRequest(context.Background(), "quicbaas.Auth", verb, "wrong"); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s wrong type = %v, want invalid input", verb, err)
		}
	}
}
