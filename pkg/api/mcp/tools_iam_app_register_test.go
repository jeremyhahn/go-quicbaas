//go:build mcp

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newRegisterToolRegistry returns a registry whose register verb reaches a
// real app manager, the manager, and the client id of the last app registered.
func newRegisterToolRegistry(t *testing.T) (*ToolRegistry, *iam.AppManager, *string) {
	t.Helper()
	apps := iam.NewAppManager(iam.NewMemoryStorage(), nil)
	var lastClientID string
	reg := newIAMTestRegistry(&mockIAMService{
		registerAppFunc: func(ctx context.Context, req *service.RegisterAppRequest) (*service.RegisterAppResponse, error) {
			app, err := apps.RegisterApp(ctx, req.TenantID, iam.RegisterAppOptions{
				Name:                    req.Name,
				RedirectURIs:            req.RedirectURIs,
				GrantTypes:              req.GrantTypes,
				TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
			})
			if err != nil {
				return nil, err
			}
			lastClientID = app.ClientID
			return &service.RegisterAppResponse{App: app, ClientSecret: app.ClientSecret}, nil
		},
	})
	return reg, apps, &lastClientID
}

// TestAppRegister_TokenEndpointAuthMethod proves the register tool carries
// token_endpoint_auth_method: client_secret_post is stored, an omitted method
// is stored as client_secret_basic, and an unsupported one is refused with
// the manager's typed error (#1473).
func TestAppRegister_TokenEndpointAuthMethod(t *testing.T) {
	reg, apps, lastClientID := newRegisterToolRegistry(t)
	register := func(method string) error {
		args := map[string]interface{}{
			"tenant_id":     "t1",
			"name":          "MyApp",
			"redirect_uris": []interface{}{"https://app.example/callback"},
			"grant_types":   []interface{}{"authorization_code"},
		}
		if method != "" {
			args["token_endpoint_auth_method"] = method
		}
		_, err := reg.executeIAMTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_iam_app_register", args)
		return err
	}

	for method, want := range map[string]string{
		iam.AuthMethodClientSecretPost: iam.AuthMethodClientSecretPost,
		"":                             iam.AuthMethodClientSecretBasic,
	} {
		require.NoError(t, register(method))
		app, err := apps.GetAppInTenant(context.Background(), "t1", *lastClientID)
		require.NoError(t, err)
		assert.Equal(t, want, app.TokenEndpointAuthMethod, "registered with %q", method)
	}

	assert.ErrorIs(t, register("client_secret_magic"), iam.ErrInvalidTokenEndpointAuthMethod)
}
