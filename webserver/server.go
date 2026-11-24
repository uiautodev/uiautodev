package webserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/uiautodev/uiautodev/version"
	"github.com/uiautodev/uiautodev/webserver/cluster"
	"github.com/uiautodev/uiautodev/webserver/device"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type APIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type APIDevice struct {
	Platform    string         `json:"platform"`
	ID          string         `json:"id"`
	Props       map[string]any `json:"props"`
	Disabled    bool           `json:"disabled"`
	Meta        *DeviceMeta    `json:"meta,omitempty"`
	SlaveID     string         `json:"slave_id,omitempty"`
	SlaveName   string         `json:"slave_name,omitempty"`
	Remote      bool           `json:"remote"`
	SlaveOnline bool           `json:"slave_online"`
	// Chain lists the cluster node IDs the device came through (see
	// cluster.Device.Chain); it is used to display nested aggregation and to
	// stop cycles.
	Chain []string `json:"chain,omitempty"`
	// Occupancy is the device presence snapshot for the requesting identity.
	// It is omitted when presence management is disabled.
	Occupancy *Occupancy `json:"occupancy,omitempty"`
}

func deviceTypeForPlatform(platform string) (device.DeviceType, bool) {
	switch platform {
	case "android":
		return device.Android, true
	case "ios":
		return device.IOS, true
	case "harmony":
		return device.HarmonyOS, true
	case "demo":
		return device.Demo, true
	default:
		return device.Android, false
	}
}

// buildDeviceList returns the devices of one platform, including the devices
// aggregated from the active cluster master.
func buildDeviceList(c echo.Context, platformFilter string, deviceType device.DeviceType) ([]APIDevice, error) {
	manager := device.NewDeviceManager(deviceType)
	serials, err := manager.ListSerials()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve devices: %w", err)
	}
	var out = make([]APIDevice, 0, len(serials))
	metas, _ := getDeviceMetas()
	master := clusterMasterFrom(c)
	visited := parseVisitedNodeIDs(c)
	nodeID := ""
	if master != nil {
		nodeID = master.NodeID()
	}
	// A requester that already contains this node in its chain is this node
	// itself (or a cycle): local devices would be duplicates.
	localAllowed := nodeID == "" || !visited[nodeID]
	if localAllowed {
		for _, serial := range serials {
			props := map[string]any{
				"serial": serial,
			}
			prefix := "a-"
			switch deviceType {
			case device.IOS:
				prefix = "i-"
			case device.HarmonyOS:
				prefix = "h-"
			case device.Demo:
				prefix = "d-"
			}
			id := prefix + serial
			dev := APIDevice{Platform: platformFilter, ID: id, Props: props, Disabled: false}
			if nodeID != "" {
				dev.Chain = []string{nodeID}
			}
			if meta, ok := metas[id]; ok {
				dev.Meta = &meta
			}
			applyOccupancy(c, &dev)
			out = append(out, dev)
		}
	}
	if master != nil {
		for _, remote := range master.RemoteDevicesFiltered(platformFilter, visited) {
			props := remote.Props
			if props == nil {
				props = map[string]any{}
			}
			dev := APIDevice{
				Platform:    remote.Platform,
				ID:          remote.ID,
				Props:       props,
				Disabled:    remote.Disabled,
				SlaveID:     remote.SlaveID,
				SlaveName:   remote.SlaveName,
				Remote:      true,
				SlaveOnline: remote.SlaveOnline,
				Chain:       remote.Chain,
			}
			if meta, ok := metas[remote.ID]; ok {
				dev.Meta = &meta
			}
			applyOccupancy(c, &dev)
			out = append(out, dev)
		}
	}
	return out, nil
}

// applyOccupancy attaches the presence snapshot of a device for the requesting
// identity. It is a no-op when presence management is disabled.
func applyOccupancy(c echo.Context, dev *APIDevice) {
	if !getAccessConfig().Presence.Enabled {
		return
	}
	identity, ok := identityFrom(c)
	if !ok {
		return
	}
	occ := presenceManagerInstance.Occupancy(dev.ID, &identity.Identity, identity.SessionID)
	dev.Occupancy = &occ
}

func devicesHandler(c echo.Context) error {
	platformFilter := c.QueryParam("platform")
	deviceType, ok := deviceTypeForPlatform(platformFilter)
	if !ok {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid platform filter",
		}, "  ")
	}

	result, err := buildDeviceList(c, platformFilter, deviceType)
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: err.Error(),
		}, "  ")
	}

	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    result,
	}, "  ")
}

func deviceConnectHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	dev, err := deviceManagerInstance.GetDevice(deviceId, deviceCacheTimeout())
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid device: " + err.Error(),
		}, "  ")
	}
	connectInfo, err := dev.AcquireDeviceConnect()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to connect device: " + err.Error(),
		}, "  ")
	}
	host := c.Request().Host
	if strings.Contains(host, ":") {
		host = strings.SplitN(host, ":", 2)[0]
	}
	port := connectInfo.Endpoint
	if strings.Contains(port, ":") {
		port = strings.SplitN(port, ":", 2)[1]
	}
	connectInfo.Endpoint = host + ":" + port
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    connectInfo,
	}, "  ")
}

func deviceDeleteHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	presenceManagerInstance.ReleaseDevice(deviceId)
	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
	}, "  ")
}

func deviceInfoHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	if master := getActiveClusterMaster(); master != nil {
		if _, _, ok := cluster.ParseGlobalDeviceID(deviceId); ok {
			remote, found := master.Device(deviceId)
			if !found {
				return c.JSONPretty(http.StatusNotFound, APIResponse{Code: 404, Message: "device not found"}, "  ")
			}
			props := remote.Props
			if props == nil {
				props = map[string]any{}
			}
			apiDevice := APIDevice{
				Platform:    remote.Platform,
				ID:          remote.ID,
				Props:       props,
				Disabled:    remote.Disabled,
				SlaveID:     remote.SlaveID,
				SlaveName:   remote.SlaveName,
				Remote:      true,
				SlaveOnline: remote.SlaveOnline,
			}
			if metas, err := getDeviceMetas(); err == nil {
				if meta, ok := metas[deviceId]; ok {
					apiDevice.Meta = &meta
				}
			}
			applyOccupancy(c, &apiDevice)
			return c.JSONPretty(http.StatusOK, APIResponse{Code: 0, Message: "ok", Data: apiDevice}, "  ")
		}
	}
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid device: " + err.Error(),
		}, "  ")
	}

	info := dev.Info()
	if info.Props == nil {
		return c.JSONPretty(http.StatusNotFound, APIResponse{
			Code:    404,
			Message: "device not found",
		}, "  ")
	}

	deviceIDInfo, _ := device.ParseDeviceID(deviceId)
	info.Platform = deviceIDInfo.Type.String()

	apiDevice := APIDevice{
		Platform: info.Platform,
		ID:       info.ID,
		Props:    info.Props,
		Disabled: info.Disabled,
	}
	if metas, err := getDeviceMetas(); err == nil {
		if meta, ok := metas[deviceId]; ok {
			apiDevice.Meta = &meta
		}
	}
	applyOccupancy(c, &apiDevice)

	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    apiDevice,
	}, "  ")
}

func deviceWindowSizeHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid device: " + err.Error(),
		}, "  ")
	}

	w, h, err := dev.GetScreenSize()
	if err != nil {
		return c.JSONPretty(http.StatusInternalServerError, APIResponse{
			Code:    500,
			Message: "failed to get window size: " + err.Error(),
		}, "  ")
	}

	return c.JSONPretty(http.StatusOK, APIResponse{
		Code:    0,
		Message: "ok",
		Data:    device.ScreenSize{Width: w, Height: h},
	}, "  ")
}

// update global.d.ts if adding new fields here
type ServerConfig struct {
	DisableDeviceListing bool          `json:"disable_device_listing"`
	Version              string        `json:"version"`
	BuildTime            string        `json:"build_time"`
	GitCommit            string        `json:"git_commit"`
	ProxyLogPath         string        `json:"-"` // Path to proxy log file, empty to disable
	AuthUsers            []string      `json:"-"` // List of user:pass strings for auth
	JWTSecretBytes       []byte        `json:"-"` // Derived secret, populated during server creation
	CORS                 bool          `json:"-"` // Whether to enable CORS for cross-origin requests
	DisablePluginAuth    bool          `json:"-"` // Whether plugin management requires no password (trusted desktop env)
	Audience             string        `json:"-"` // App audience: "desktop" (wails) or "server" (standalone)
	NodeID               string        `json:"-"` // Cluster node identity override (tests); empty means persisted config value
	APIToken             string        `json:"-"` // Per-process token that guards cluster management routes (desktop, CORS enabled)
	ClusterStore         cluster.Store `json:"-"` // Cluster slave store override (tests); nil uses the config file
	TLSAddr              string        `json:"-"` // HTTPS listen address (enables TLS when set)
	TLSCertFile          string        `json:"-"` // Path to HTTPS certificate file (PEM)
	TLSKeyFile           string        `json:"-"` // Path to HTTPS private key file (PEM)
	Quit                 func()        `json:"-"` // Exits the desktop app after installing an update
	OnLocaleChange       func(string)  `json:"-"` // Rebuilds native menus after the UI language changes
	StartIOSWDAWatcher   bool          `json:"-"` // Starts WebDriverAgent for connected iOS devices in the background
}

var defaultServerConfig = ServerConfig{
	DisableDeviceListing: false,
	Version:              version.Version,
	Audience:             "server",
}

func configHandler(config ServerConfig) echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSONPretty(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data:    config,
		}, "  ")
	}
}

// setupMiddleware configures and applies middleware to the Echo instance
func setupMiddleware(e *echo.Echo, config ServerConfig) {
	// Server-Timing is registered first so it is the outermost middleware and
	// even panic/404 responses report how long the server spent on them.
	e.Use(serverTiming())

	// Recovery middleware to recover from panics
	e.Use(middleware.Recover())

	// Logger middleware to log HTTP requests (redacts token query params)
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `{"time":"${time_rfc3339_nano}","id":"${id}","remote_ip":"${remote_ip}",` +
			`"host":"${host}","method":"${method}","uri":"${custom}","user_agent":"${user_agent}",` +
			`"status":${status},"error":"${error}","latency":${latency},"latency_human":"${latency_human}"` +
			`,"bytes_in":${bytes_in},"bytes_out":${bytes_out}}` + "\n",
		CustomTagFunc: func(c echo.Context, buf *bytes.Buffer) (int, error) {
			return buf.WriteString(redactTokenQuery(c.Request().RequestURI))
		},
	}))

	// CORS middleware to allow cross-origin requests (opt-in via --cors).
	// Registered before the auth middleware so preflight OPTIONS requests are
	// answered without requiring credentials.
	if config.CORS {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			AllowOriginFunc: func(origin string) (bool, error) {
				return true, nil
			},
			AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
			AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, apiTokenHeader},
			// The dashboard runs from a different origin in server mode, so
			// without this the Server Timing API would stay invisible there.
			ExposeHeaders: []string{serverTimingHeader},
		}))
	}

	// JWT cookie auth middleware - only enabled if AuthUsers is configured
	if len(config.AuthUsers) > 0 && config.JWTSecretBytes != nil { // pragma: allowlist secret
		e.Use(JWTAuthMiddleware(config.AuthUsers, config.JWTSecretBytes))
		slog.Info("JWT cookie auth enabled", "users", len(config.AuthUsers))
	}
}

func CreateEchoServer() *echo.Echo {
	return CreateEchoServerWithConfig(defaultServerConfig)
}

func CreateEchoServerWithConfig(config ServerConfig) *echo.Echo {
	config.Version = version.Version
	config.BuildTime = version.BuildTime
	config.GitCommit = version.GitCommit
	if config.Audience == "" {
		config.Audience = "server"
	}

	configureUpdateClient(config)
	if config.StartIOSWDAWatcher {
		device.StartIOSWDAWatcher(context.Background(), 0)
	}

	configJSON, _ := json.Marshal(config)
	slog.Info("server config", "config", string(configJSON))

	// Initialize proxy logger if path is configured
	if config.ProxyLogPath != "" {
		if err := InitProxyLogger(config.ProxyLogPath); err != nil {
			slog.Error("Failed to initialize proxy logger", "error", err)
		} else {
			slog.Info("Proxy logger initialized", "path", config.ProxyLogPath)
		}
	}

	// Load JWT secret before middleware setup so it's available for route registration
	if len(config.AuthUsers) > 0 {
		jwtSecretBytes, err := loadOrCreateJWTSecret()
		if err != nil {
			// Auth is configured; refuse to start without a signing key to avoid running unauthenticated.
			slog.Error("failed to initialize JWT secret; refusing to start with auth enabled", "error", err)
			panic(err)
		}
		config.JWTSecretBytes = jwtSecretBytes // pragma: allowlist secret

		// With login auth enabled, /mcp/ should be guarded: ensure an MCP token
		// exists, generating one on first start. A failure is non-fatal — /mcp/
		// then falls back to login auth.
		if _, err := loadOrCreateMCPToken(); err != nil {
			slog.Error("failed to initialize MCP token; /mcp/ will fall back to login auth", "error", err)
		}
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Setup middleware (recover, logger, jwt cookie auth)
	setupMiddleware(e, config)

	// Identity resolution runs after the optional JWT auth so it can reuse the
	// username it sets, then presence auto-sessions device requests.
	e.Use(identityMiddleware())
	device.SetStreamConnHook(RegisterStreamConn)
	device.SetStreamReadOnlyHook(StreamReadOnly)
	e.Use(presenceMiddleware(presenceManagerInstance))

	// Routes
	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{"ok": true, "ts": time.Now().Unix()})
	})
	e.GET("/api/v1/config", configHandler(config))
	e.GET("/api/v1/me", meHandler)
	e.GET("/api/v1/access/config", accessConfigGetHandler, managementAuthMiddleware(config))
	e.PUT("/api/v1/access/config", accessConfigPutHandler, managementAuthMiddleware(config))
	e.GET("/api/v1/presence", presenceSnapshotHandler)
	e.PUT("/api/v1/presence", presenceBatchPutHandler)
	e.PUT("/api/v1/devices/:device_id/presence", presencePutHandler)
	e.DELETE("/api/v1/devices/:device_id/presence", presenceDeleteHandler)
	e.GET("/api/v1/locale", localeGetHandler())
	e.PUT("/api/v1/locale", localePutHandler(config))
	e.GET("/api/v1/version/latest", versionLatestHandler())
	e.GET("/api/v1/update", updateInfoHandler())
	e.POST("/api/v1/update", updateApplyHandler(), managementAuthMiddleware(config))
	e.GET("/api/v1/update/status", updateStatusHandler())
	e.GET("/api/v1/mcp/token", mcpTokenGetHandler, managementAuthMiddleware(config))
	e.PUT("/api/v1/mcp/token", mcpTokenSetHandler, managementAuthMiddleware(config))
	if !config.DisableDeviceListing {
		e.GET("/api/v1/devices", devicesHandler)
		e.GET("/stream/devices", deviceListStreamHandler)
	}
	e.GET("/api/v1/devices/:device_id", deviceInfoHandler)
	e.GET("/api/v1/devices/:device_id/meta", deviceMetaGetHandler)
	e.PUT("/api/v1/devices/:device_id/meta", deviceMetaPutHandler)
	e.DELETE("/api/v1/devices/:device_id/meta", deviceMetaDeleteHandler)
	e.GET("/api/v1/devices/:device_id/screenshot", deviceScreenshotHandler)
	e.GET("/api/v1/devices/:device_id/screens", deviceScreensHandler)
	e.GET("/api/v1/devices/:device_id/clipboard", deviceClipboardGetHandler)
	e.PUT("/api/v1/devices/:device_id/clipboard", deviceClipboardPutHandler)
	e.GET("/api/v1/devices/:device_id/window/size", deviceWindowSizeHandler)
	e.GET("/api/v1/devices/:device_id/encoders", deviceEncodersHandler)
	e.GET("/api/v1/devices/:device_id/apps", appsListHandler)
	e.GET("/api/v1/devices/:device_id/apps/current", appCurrentHandler)
	e.POST("/api/v1/devices/:device_id/apps/:app_id/start", appStartHandler)
	e.POST("/api/v1/devices/:device_id/apps/:app_id/stop", appStopHandler)
	e.GET("/api/v1/devices/:device_id/uitree", deviceUITreeHandler)
	// Deprecated: use /uitree instead. Kept for backward compatibility.
	e.GET("/api/v1/devices/:device_id/ui/tree", deviceUITreeHandler)
	e.GET("/api/v1/devices/:device_id/connect", deviceConnectHandler)
	e.DELETE("/api/v1/devices/:device_id/connect", deviceDeleteHandler)
	e.POST("/api/v1/devices/:device_id/shell", deviceShellHandler)
	e.POST("/api/v1/devices/:device_id/actions/tap", deviceTapHandler)
	e.POST("/api/v1/devices/:device_id/actions/swipe", deviceSwipeHandler)
	e.POST("/api/v1/devices/:device_id/actions/press_key", devicePressKeyHandler)
	e.POST("/api/v1/devices/:device_id/actions/input_text", deviceInputTextHandler)
	e.POST("/api/v1/devices/:device_id/actions/clear_text", deviceClearTextHandler)
	e.GET("/stream/devices/:device_id/screen/scrcpy", device.WSScrcpyHandler)
	e.GET("/stream/devices/:device_id/screen/mjpeg", device.MJPEGHandler)
	e.GET("/stream/devices/:device_id/screen/frames", device.WSScreenFramesHandler)
	e.GET("/stream/devices/:device_id/terminal", device.WSTerminalHandler)
	// UI tree routes
	e.POST("/api/v1/ui/xpath_query", uiXPathQueryHandler)
	e.POST("/api/v1/ui/xml", uiXMLHandler)
	e.Any("/proxy/request/*", proxyRequestHandler)
	e.Any("/proxy/devices/:device_id/:socket_addr/*", deviceProxyHandler)
	mcpHandler := echo.WrapHandler(buildSSEServer(config.DisableDeviceListing))
	// Only register /mcp/* (not the exact /mcp path) so the standalone server's
	// frontend /mcp page route is not shadowed. All MCP endpoints live under
	// /mcp/ (sse, message, http).
	e.Any("/mcp/*", mcpHandler, MCPAuthMiddleware())
	// e.Any("/proxy/*", echo.WrapHandler(http.HandlerFunc(handleProxy)))

	// Plugin routes
	e.GET("/api/v1/plugins", pluginsHandler)
	e.POST("/api/v1/plugins", pluginInstallHandler, managementAuthMiddleware(config))
	e.GET("/api/v1/plugins/auth", managementAuthStatusHandler)
	e.POST("/api/v1/plugins/auth", managementAuthLoginHandler)
	e.POST("/api/v1/plugins/auth/setup", managementAuthSetupHandler)
	e.PUT("/api/v1/plugins/:id", pluginSetEnabledHandler, managementAuthMiddleware(config))
	e.DELETE("/api/v1/plugins/:id", pluginUninstallHandler, managementAuthMiddleware(config))

	// Plugin market routes (client side of the registry, guarded by management auth)
	e.GET("/api/v1/market/plugins", marketListHandler(), managementAuthMiddleware(config))
	e.POST("/api/v1/market/plugins/:id/install", marketInstallHandler(), managementAuthMiddleware(config))
	e.GET("/api/v1/market/source", marketSourceGetHandler(), managementAuthMiddleware(config))
	e.PUT("/api/v1/market/source", marketSourcePutHandler(), managementAuthMiddleware(config))

	// OpenAI provider config routes (guarded by the same management auth)
	e.GET("/api/v1/openai/config", openaiConfigGetHandler, managementAuthMiddleware(config))
	e.PUT("/api/v1/openai/config", openaiConfigPutHandler, managementAuthMiddleware(config))
	e.POST("/api/v1/openai/models", openaiModelsHandler, managementAuthMiddleware(config))
	// Chat completions proxy for plugins (open to plugins, no session token).
	e.POST("/api/v1/openai/chat/completions", openAIChatCompletionsHandler)

	// Jev provider config routes (guarded by the same management auth)
	e.GET("/api/v1/jev/config", jevConfigGetHandler, managementAuthMiddleware(config))
	e.PUT("/api/v1/jev/config", jevConfigPutHandler, managementAuthMiddleware(config))
	// Jev decision proxy for plugins (open to plugins, no session token).
	e.POST("/api/v1/jev/decisions", jevDecisionsHandler)

	// Serve plugin runtime type declarations and schema
	e.GET("/plugin-runtime.d.ts", serveRuntimeDTS)
	e.GET("/plugin.schema.json", servePluginSchema)

	// Serve plugin runtime JS (must be registered before the catch-all)
	e.GET("/plugins/:id/__runtime.js", func(c echo.Context) error {
		return GetPluginManager().serveRuntimeJS(c, c.Param("id"))
	})

	// Serve plugin static files from multiple directories (catch-all)
	e.GET("/plugins/:id/*", GetPluginManager().FileHandler())

	if previous := getActiveClusterMaster(); previous != nil {
		previous.Close()
	}
	setActiveClusterMaster(nil)
	device.RegisterRemoteDeviceResolver(nil)

	// Every node is a cluster master so it can aggregate slaves and be used as
	// a slave by an upstream master at the same time. The slave inventory
	// endpoint stays available for push-mode upstream masters.
	store := config.ClusterStore
	if store == nil {
		store = newClusterStore()
	}
	master := cluster.NewMaster(store, cluster.MasterConfig{
		Version: version.Version,
		NodeID:  resolveNodeID(config.NodeID),
		Relay:   getClusterADBRelayConfig(),
	})
	setActiveClusterMaster(master)
	device.RegisterRemoteDeviceResolver(master.ResolveDevice)
	// Handlers read the master from the request context so multiple servers can
	// coexist in one process (tests) without fighting over the global.
	e.Use(clusterContextMiddleware(master))
	clusterAuth := managementAuthMiddleware(config)
	if config.APIToken != "" {
		// The desktop server is CORS-enabled and has no management password, so
		// any web origin could otherwise mutate cluster settings. Require the
		// per-process token that is only available through the Wails bridge.
		clusterAuth = requireAPIToken(config.APIToken, clusterAuth)
	}
	master.RegisterRoutes(e, clusterAuth)
	e.Use(master.DeviceRouteMiddleware())
	e.GET("/api/v1/cluster/devices", clusterDevicesHandler())
	e.GET("/api/v1/cluster/info", clusterInfoHandler())
	return e
}

// apiTokenHeader carries the per-process desktop API token.
const apiTokenHeader = "X-Uiautodev-Token"

// requireAPIToken wraps next (which may be nil) with a constant-time token check
// for state-changing requests. Reads stay open so the settings page can poll
// before the desktop token has been loaded.
func requireAPIToken(token string, next echo.MiddlewareFunc) echo.MiddlewareFunc {
	return func(handler echo.HandlerFunc) echo.HandlerFunc {
		guarded := func(c echo.Context) error {
			switch c.Request().Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				return handler(c)
			}
			got := c.Request().Header.Get(apiTokenHeader)
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				return c.JSONPretty(http.StatusUnauthorized, APIResponse{
					Code:    401,
					Message: "unauthorized: valid api token required",
				}, "  ")
			}
			return handler(c)
		}
		if next == nil {
			return guarded
		}
		return next(guarded)
	}
}

// CloseCluster closes the active cluster master (tunnel sessions, direct-slave
// pollers and adb relay listeners).
func CloseCluster() {
	if master := getActiveClusterMaster(); master != nil {
		master.Close()
	}
}

const clusterMasterContextKey = "cluster-master"

// clusterContextMiddleware makes the server's own master available to handlers.
func clusterContextMiddleware(master *cluster.Master) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(clusterMasterContextKey, master)
			return next(c)
		}
	}
}

// clusterMasterFrom returns the master serving the current request, falling back
// to the process-wide master for handlers invoked outside a server.
func clusterMasterFrom(c echo.Context) *cluster.Master {
	if master, ok := c.Get(clusterMasterContextKey).(*cluster.Master); ok {
		return master
	}
	return getActiveClusterMaster()
}

// resolveNodeID returns the cluster node identity, generating and persisting one
// when no override is provided.
func resolveNodeID(override string) string {
	if override != "" {
		return override
	}
	nodeID, err := loadOrCreateNodeID()
	if err != nil {
		slog.Warn("cluster: failed to load node id", "error", err)
		return ""
	}
	return nodeID
}

// ClusterNodeID returns the local node identity. It is read from the active
// master when one exists (so tests that override the id stay consistent), and
// otherwise resolved from the persisted config.
func ClusterNodeID() string {
	if master := getActiveClusterMaster(); master != nil {
		return master.NodeID()
	}
	return resolveNodeID("")
}

// clusterInfoHandler exposes the node identity to upstream masters.
func clusterInfoHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		nodeID := ""
		if master := clusterMasterFrom(c); master != nil {
			nodeID = master.NodeID()
		}
		return c.JSON(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data: map[string]any{
				"node_id":   nodeID,
				"is_master": true,
				"version":   version.Version,
			},
		})
	}
}

// parseVisitedNodeIDs reads the comma separated `visited` query parameter that
// upstream masters use to prevent aggregation cycles.
func parseVisitedNodeIDs(c echo.Context) map[string]bool {
	raw := c.QueryParam("visited")
	if raw == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Uiautodev Self-Signed"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key, // pragma: allowlist secret
	}, nil
}

func startRedirectServer(httpAddr, httpsAddr string) {
	_, httpsPort, err := net.SplitHostPort(httpsAddr)
	if err != nil {
		slog.Error("invalid TLS listen address", "addr", httpsAddr, "err", err)
		return
	}
	redirectSrv := &http.Server{
		Addr: httpAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hostname, _, err := net.SplitHostPort(r.Host)
			if err != nil {
				hostname = r.Host
			}
			target := "https://" + hostname + ":" + httpsPort + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		}),
	}
	slog.Info("HTTP redirect server listening", "addr", httpAddr, "to", httpsAddr)
	if err := redirectSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP redirect server error", "err", err)
	}
}

func StartWithConfig(e *echo.Echo, httpAddr string, cfg ServerConfig) error {
	if cfg.TLSAddr != "" {
		// Start HTTP redirect server on httpAddr
		if httpAddr != "" {
			go startRedirectServer(httpAddr, cfg.TLSAddr)
		}

		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			slog.Info("HTTPS server listening", "addr", cfg.TLSAddr)
			return e.StartTLS(cfg.TLSAddr, cfg.TLSCertFile, cfg.TLSKeyFile)
		}
		cert, err := generateSelfSignedCert()
		if err != nil {
			return fmt.Errorf("generate self-signed cert: %w", err)
		}
		slog.Info("HTTPS server (self-signed cert) listening", "addr", cfg.TLSAddr)
		server := &http.Server{
			Addr:    cfg.TLSAddr,
			Handler: e,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
			},
		}
		return e.StartServer(server)
	}
	slog.Info("HTTP server listening", "addr", httpAddr)
	return e.Start(httpAddr)
}

// Returns a shutdown function that gracefully stops the server.
func StartHTTPServer(addr string) func() error {
	e := CreateEchoServer()
	// Start server
	go func() {
		log.Println("HTTP API (echo) listening on", addr)
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Println("echo server error:", err)
		}
	}()

	// Return shutdown function
	return func() error {
		log.Println("Manually shutting down HTTP API server...")
		if master := getActiveClusterMaster(); master != nil {
			master.Close()
		}
		CloseProxyLogger()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return e.Shutdown(ctx)
	}
}
