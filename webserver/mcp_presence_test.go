package webserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMCPIdentity(id, session string) *requestIdentity {
	return &requestIdentity{
		Identity:  Identity{ID: id, Name: id},
		SessionID: session,
		Source:    AuthSourceMCP,
	}
}

func mcpContextWithTestIdentity(identity *requestIdentity) context.Context {
	return context.WithValue(context.Background(), mcpIdentityContextKey{}, identity)
}

func TestMCPAuthMiddlewareInjectsAdminIdentity(t *testing.T) {
	isolateConfig(t)
	require.NoError(t, setMCPToken("admin-token"))
	t.Cleanup(func() { _ = setMCPToken("") })

	e := echo.New()
	e.Use(identityMiddleware())
	e.GET("/mcp/whoami", func(c echo.Context) error {
		identity, ok := identityFrom(c)
		if !ok {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": "missing identity"})
		}
		ctxIdentity := mcpIdentityFromContext(c.Request().Context())
		return c.JSON(http.StatusOK, map[string]any{
			"id":        identity.ID,
			"name":      identity.Name,
			"source":    identity.Source,
			"session":   identity.SessionID,
			"anonymous": identity.Anonymous,
			"ctx_id":    ctxIdentity.ID,
		})
	}, MCPAuthMiddleware())

	do := func() map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/mcp/whoami?token=admin-token", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return body
	}

	first := do()
	assert.Equal(t, mcpIdentityID, first["id"])
	assert.Equal(t, mcpIdentityName, first["name"])
	assert.Equal(t, string(AuthSourceMCP), first["source"])
	assert.Equal(t, false, first["anonymous"])
	assert.Equal(t, mcpIdentityID, first["ctx_id"])
	assert.NotEmpty(t, first["session"])

	second := do()
	assert.Equal(t, first["session"], second["session"], "the token should yield a stable session")
}

func TestMCPPresenceConflictTakeoverAndRelease(t *testing.T) {
	isolateConfig(t)
	const deviceID = "a-mcp-conflict"
	t.Cleanup(func() { presenceManagerInstance.ReleaseDevice(deviceID) })

	bob := testMCPIdentity("anon:bob", "sess-bob")
	admin := testMCPIdentity(mcpIdentityID, "sess-admin")

	occ, perr := mcpEnsureDeviceSession(mcpContextWithTestIdentity(bob), deviceID, true)
	require.Nil(t, perr)
	require.Equal(t, OccupancyMine, occ.State)

	// The MCP admin cannot control a device bob owns without a takeover.
	occ, perr = mcpEnsureDeviceSession(mcpContextWithTestIdentity(admin), deviceID, true)
	require.NotNil(t, perr)
	assert.Equal(t, "device_busy", perr.Code)
	assert.True(t, occ.CanTakeover)

	// Read-only tools join as an observer.
	occ, perr = mcpEnsureDeviceSession(mcpContextWithTestIdentity(admin), deviceID, false)
	require.Nil(t, perr)
	assert.Equal(t, OccupancyViewing, occ.State)

	// An observer cannot call control tools.
	_, perr = mcpEnsureDeviceSession(mcpContextWithTestIdentity(admin), deviceID, true)
	require.NotNil(t, perr)
	assert.Equal(t, "read_only", perr.Code)

	// Explicit takeover moves control to the MCP identity and evicts bob.
	acquireReq := mcp.CallToolRequest{}
	acquireReq.Params.Name = toolAcquireDevice
	acquireReq.Params.Arguments = map[string]any{
		argDeviceID: deviceID,
		argMode:     string(AccessModeControl),
		argTakeover: true,
	}
	acquired, err := mcpAcquireDeviceHandler(mcpContextWithTestIdentity(admin), acquireReq)
	require.NoError(t, err)
	require.False(t, acquired.IsError)
	assert.Contains(t, acquired.Content[0].(mcp.TextContent).Text, `"state":"mine"`)

	_, perr = mcpEnsureDeviceSession(mcpContextWithTestIdentity(bob), deviceID, false)
	require.NotNil(t, perr)
	assert.Equal(t, "preempted", perr.Code)

	// Release frees the device for another identity.
	releaseReq := mcp.CallToolRequest{}
	releaseReq.Params.Name = toolReleaseDevice
	releaseReq.Params.Arguments = map[string]any{argDeviceID: deviceID}
	result, err := mcpReleaseDeviceHandler(mcpContextWithTestIdentity(admin), releaseReq)
	require.NoError(t, err)
	require.False(t, result.IsError)

	carol := testMCPIdentity("anon:carol", "sess-carol")
	occ, perr = mcpEnsureDeviceSession(mcpContextWithTestIdentity(carol), deviceID, true)
	require.Nil(t, perr)
	assert.Equal(t, OccupancyMine, occ.State)
}

func TestMCPAcquireTool(t *testing.T) {
	isolateConfig(t)
	const deviceID = "a-mcp-acquire"
	t.Cleanup(func() { presenceManagerInstance.ReleaseDevice(deviceID) })

	admin := testMCPIdentity(mcpIdentityID, "sess-admin")
	req := mcp.CallToolRequest{}
	req.Params.Name = toolAcquireDevice
	req.Params.Arguments = map[string]any{argDeviceID: deviceID, argMode: string(AccessModeView)}

	result, err := mcpAcquireDeviceHandler(mcpContextWithTestIdentity(admin), req)
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected a text result")
	assert.True(t, strings.Contains(text.Text, `"state":"viewing"`), text.Text)

	// Invalid mode is rejected as a tool error.
	req.Params.Arguments = map[string]any{argDeviceID: deviceID, argMode: "observe"}
	_, err = mcpAcquireDeviceHandler(mcpContextWithTestIdentity(admin), req)
	require.Error(t, err)
}

func TestMCPPresenceDisabled(t *testing.T) {
	isolateConfig(t)
	cfg := defaultAccessConfig()
	cfg.Presence.Enabled = false
	_, err := setAccessConfig(cfg)
	require.NoError(t, err)

	admin := testMCPIdentity(mcpIdentityID, "sess-admin")
	occ, perr := mcpEnsureDeviceSession(mcpContextWithTestIdentity(admin), "a-mcp-disabled", true)
	require.Nil(t, perr)
	assert.Equal(t, OccupancyFree, occ.State)
}

func TestMCPToolGateClassification(t *testing.T) {
	gate := newMCPToolGate()
	gate.register(mcp.NewTool("screenshot", mcp.WithReadOnlyHintAnnotation(true)))
	gate.register(mcp.NewTool("tap"))
	gate.register(mcp.NewTool(toolAcquireDevice))
	gate.register(mcp.NewTool(toolReleaseDevice))

	assert.True(t, gate.isReadOnly("screenshot"))
	assert.False(t, gate.isReadOnly("tap"))
	assert.True(t, gate.isSkipped(toolAcquireDevice))
	assert.True(t, gate.isSkipped(toolReleaseDevice))
	assert.False(t, gate.isSkipped("tap"))
}

func TestMCPToolsIncludePresenceTools(t *testing.T) {
	mux := buildSSEServer(false)
	req := httptest.NewRequest(http.MethodGet, "/mcp/tools", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	names := make([]string, 0, len(body.Data))
	for _, tool := range body.Data {
		names = append(names, tool.Name)
	}
	assert.Contains(t, names, toolAcquireDevice)
	assert.Contains(t, names, toolReleaseDevice)
}

// TestRequireIdentityDoesNotBlockMCPToken keeps require_identity from locking
// out legitimate MCP clients: their token is an identity source of its own and
// MCPAuthMiddleware runs after the global identity middleware.
func TestRequireIdentityDoesNotBlockMCPToken(t *testing.T) {
	isolateConfig(t)
	require.NoError(t, setMCPToken("u1u-required"))
	t.Cleanup(func() { _ = setMCPToken("") })

	cfg := defaultAccessConfig()
	cfg.Auth.RequireIdentity = true
	_, err := setAccessConfig(cfg)
	require.NoError(t, err)

	e := echo.New()
	e.Use(identityMiddleware())
	e.GET("/api/v1/devices", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.POST("/mcp/http", func(c echo.Context) error {
		identity, ok := identityFrom(c)
		if !ok {
			return c.JSON(http.StatusInternalServerError, map[string]any{"error": "no identity"})
		}
		return c.JSON(http.StatusOK, map[string]string{"id": identity.ID})
	}, MCPAuthMiddleware())

	apiReq := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	apiRec := httptest.NewRecorder()
	e.ServeHTTP(apiRec, apiReq)
	require.Equal(t, http.StatusUnauthorized, apiRec.Code, apiRec.Body.String())

	mcpReq := httptest.NewRequest(http.MethodPost, "/mcp/http", strings.NewReader("{}"))
	mcpReq.Header.Set("Authorization", "Bearer u1u-required")
	mcpRec := httptest.NewRecorder()
	e.ServeHTTP(mcpRec, mcpReq)
	require.Equal(t, http.StatusOK, mcpRec.Code, mcpRec.Body.String())

	var body map[string]string
	require.NoError(t, json.Unmarshal(mcpRec.Body.Bytes(), &body))
	assert.Equal(t, mcpIdentityID, body["id"])
}

// TestMCPToolGateRejectsControlToolsOverTransport covers the gate end to end: a
// control tool called from the real streamable HTTP transport must fail with
// device_busy when another identity holds the device and with read_only when the
// MCP session is observing.
func TestMCPToolGateRejectsControlToolsOverTransport(t *testing.T) {
	isolateConfig(t)
	require.NoError(t, setMCPToken("u1u-gate"))
	const deviceID = "a-mcp-gate"
	t.Cleanup(func() {
		_ = setMCPToken("")
		presenceManagerInstance.ReleaseDevice(deviceID)
	})

	e := echo.New()
	e.Use(identityMiddleware())
	e.Any("/mcp/*", echo.WrapHandler(buildSSEServer(false)), MCPAuthMiddleware())
	srv := httptest.NewServer(e)
	defer srv.Close()

	mcpClient, err := client.NewStreamableHttpClient(
		srv.URL+"/mcp/http",
		transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer u1u-gate"}),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, mcpClient.Start(ctx))
	_, err = mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "presence-gate-test", Version: "1.0.0"},
		},
	})
	require.NoError(t, err)

	tap := mcp.CallToolRequest{}
	tap.Params.Name = "tap"
	tap.Params.Arguments = map[string]any{argDeviceID: deviceID, "x": 1.0, "y": 2.0}

	// Another identity controls the device: the control tool is refused.
	alice := testIdentity("anon:alice", "sess-alice")
	_, perr := presenceManagerInstance.Acquire(deviceID, alice, AccessModeControl, false, "ip", "ua")
	require.Nil(t, perr)
	result, err := mcpClient.CallTool(ctx, tap)
	require.NoError(t, err)
	require.True(t, result.IsError, "tap should be refused while the device is busy: %+v", result.Content)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, `"code":"device_busy"`)

	// The MCP session joins as an observer; control tools are read_only.
	presenceManagerInstance.ReleaseDevice(deviceID)
	acquire := mcp.CallToolRequest{}
	acquire.Params.Name = toolAcquireDevice
	acquire.Params.Arguments = map[string]any{argDeviceID: deviceID, argMode: string(AccessModeView)}
	acquired, err := mcpClient.CallTool(ctx, acquire)
	require.NoError(t, err)
	require.False(t, acquired.IsError, acquired.Content)

	result, err = mcpClient.CallTool(ctx, tap)
	require.NoError(t, err)
	require.True(t, result.IsError, "an observer must not control the device: %+v", result.Content)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, `"code":"read_only"`)
}

// TestMCPToolCallCarriesIdentity exercises the real streamable HTTP transport to
// make sure the identity injected by MCPAuthMiddleware reaches tool handlers.
func TestMCPToolCallCarriesIdentity(t *testing.T) {
	isolateConfig(t)
	require.NoError(t, setMCPToken("e2e-token"))
	const deviceID = "a-mcp-e2e"
	t.Cleanup(func() {
		_ = setMCPToken("")
		presenceManagerInstance.ReleaseDevice(deviceID)
	})

	e := echo.New()
	e.Use(identityMiddleware())
	e.Any("/mcp/*", echo.WrapHandler(buildSSEServer(false)), MCPAuthMiddleware())
	srv := httptest.NewServer(e)
	defer srv.Close()

	mcpClient, err := client.NewStreamableHttpClient(
		srv.URL+"/mcp/http",
		transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer e2e-token"}),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, mcpClient.Start(ctx))
	_, err = mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "presence-test", Version: "1.0.0"},
		},
	})
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = toolAcquireDevice
	request.Params.Arguments = map[string]any{argDeviceID: deviceID}
	result, err := mcpClient.CallTool(ctx, request)
	require.NoError(t, err)
	require.False(t, result.IsError, result.Content)

	occ := presenceManagerInstance.Occupancy(deviceID, &Identity{ID: mcpIdentityID}, hashShort("e2e-token"))
	assert.Equal(t, OccupancyMine, occ.State)
}
