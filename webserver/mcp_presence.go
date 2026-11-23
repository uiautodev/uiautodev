package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	toolAcquireDevice = "acquire_device"
	toolReleaseDevice = "release_device"

	argMode     = "mode"
	argTakeover = "takeover"

	// mcpRemote and mcpUA label sessions created by MCP clients in occupancy
	// listings.
	mcpRemote = "mcp"
	mcpUA     = "mcp"
)

// mcpToolGate classifies registered tools so the presence middleware can apply
// the same read/control rules as the HTTP API.
type mcpToolGate struct {
	mu       sync.RWMutex
	readOnly map[string]bool
	skip     map[string]bool
}

func newMCPToolGate() *mcpToolGate {
	return &mcpToolGate{
		readOnly: make(map[string]bool),
		skip:     make(map[string]bool),
	}
}

// register records the annotations of a tool added to the MCP server.
func (g *mcpToolGate) register(tool mcp.Tool) {
	readOnly := tool.Annotations.ReadOnlyHint != nil && *tool.Annotations.ReadOnlyHint
	g.mu.Lock()
	defer g.mu.Unlock()
	g.readOnly[tool.Name] = readOnly
	g.skip[tool.Name] = tool.Name == toolAcquireDevice || tool.Name == toolReleaseDevice
}

func (g *mcpToolGate) isReadOnly(name string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.readOnly[name]
}

func (g *mcpToolGate) isSkipped(name string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.skip[name]
}

// middleware enforces device presence for every tool that operates on a device.
func (g *mcpToolGate) middleware() server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			deviceID, _ := req.GetArguments()[argDeviceID].(string)
			if deviceID == "" || g.isSkipped(req.Params.Name) {
				return next(ctx, req)
			}
			control := !g.isReadOnly(req.Params.Name)
			if _, perr := mcpEnsureDeviceSession(ctx, deviceID, control); perr != nil {
				return mcpPresenceErrorResult(perr), nil
			}
			return next(ctx, req)
		}
	}
}

// mcpFreeOccupancy is the no-op occupancy used when presence management is
// disabled or the caller has no identity.
func mcpFreeOccupancy(deviceID string) Occupancy {
	cfg := getAccessConfig().Presence
	return Occupancy{
		DeviceID:   deviceID,
		State:      OccupancyFree,
		Policy:     cfg.ConflictPolicy,
		CanObserve: cfg.AllowObservers,
	}
}

// mcpEnsureDeviceSession acquires or refreshes a session for an implicit MCP
// tool call. Explicit acquisition is handled by mcpAcquireDeviceHandler because
// it may take a device over.
func mcpEnsureDeviceSession(ctx context.Context, deviceID string, control bool) (Occupancy, *PresenceError) {
	cfg := getAccessConfig().Presence
	if !cfg.Enabled {
		return mcpFreeOccupancy(deviceID), nil
	}
	identity := mcpIdentityFromContext(ctx)
	if identity == nil {
		return mcpFreeOccupancy(deviceID), nil
	}

	if notice := presenceManagerInstance.Eviction(deviceID, identity.SessionID); notice != nil {
		occ := presenceManagerInstance.Occupancy(deviceID, &identity.Identity, identity.SessionID)
		occ.State = OccupancyPreempted
		occ.Notice = notice
		return occ, &PresenceError{
			Status:    http.StatusConflict,
			Code:      "preempted",
			Message:   "device session was preempted",
			Occupancy: occ,
		}
	}

	if !control {
		return presenceManagerInstance.AcquireAuto(deviceID, *identity, mcpRemote, mcpUA)
	}

	if mode, ok := presenceManagerInstance.Mode(deviceID, identity.SessionID); ok && mode == AccessModeView {
		occ := presenceManagerInstance.Occupancy(deviceID, &identity.Identity, identity.SessionID)
		return occ, &PresenceError{
			Status:    http.StatusForbidden,
			Code:      "read_only",
			Message:   "view-only session cannot control the device",
			Occupancy: occ,
		}
	}
	return presenceManagerInstance.Acquire(deviceID, *identity, AccessModeControl, false, mcpRemote, mcpUA)
}

// mcpPresenceErrorResult converts a presence error into a structured tool error
// so agents can react to device_busy / read_only / preempted.
func mcpPresenceErrorResult(err *PresenceError) *mcp.CallToolResult {
	payload := map[string]any{
		"code":      err.Code,
		"message":   err.Message,
		"occupancy": err.Occupancy,
	}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return mcp.NewToolResultError(err.Code)
	}
	return mcp.NewToolResultError(string(data))
}

func mcpAcquireDeviceHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID, ok := req.GetArguments()[argDeviceID].(string)
	if !ok || deviceID == "" {
		return nil, fmt.Errorf("missing or invalid %s argument", argDeviceID)
	}

	mode := AccessModeControl
	if raw, ok := req.GetArguments()[argMode].(string); ok && raw != "" {
		mode = DeviceAccessMode(raw)
		if !mode.Valid() {
			return nil, fmt.Errorf("invalid mode %q, valid values: control, view", raw)
		}
	}
	takeover, _ := req.GetArguments()[argTakeover].(bool)

	identity := mcpIdentityFromContext(ctx)
	if identity == nil || !getAccessConfig().Presence.Enabled {
		return mcp.NewToolResultJSON(mcpFreeOccupancy(deviceID))
	}

	occ, perr := presenceManagerInstance.Acquire(deviceID, *identity, mode, takeover, mcpRemote, mcpUA)
	if perr != nil {
		return mcpPresenceErrorResult(perr), nil
	}
	return mcp.NewToolResultJSON(occ)
}

func mcpReleaseDeviceHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID, ok := req.GetArguments()[argDeviceID].(string)
	if !ok || deviceID == "" {
		return nil, fmt.Errorf("missing or invalid %s argument", argDeviceID)
	}
	if identity := mcpIdentityFromContext(ctx); identity != nil {
		presenceManagerInstance.Release(deviceID, identity.SessionID)
	}
	return mcp.NewToolResultJSON(map[string]any{
		"device_id": deviceID,
		"released":  true,
	})
}

// registerMCPPresenceTools adds the explicit acquire/release tools. They bypass
// the presence middleware because they implement its semantics themselves.
func registerMCPPresenceTools(
	addTool func(mcp.Tool, server.ToolHandlerFunc),
	deviceIDOption func() mcp.ToolOption,
) {
	addTool(mcp.NewTool(toolAcquireDevice,
		mcp.WithDescription("Acquires a device for this MCP session before controlling it. Control tools fail with device_busy when another user holds the device; set takeover=true to preempt the current controller when the conflict policy allows it. Read-only tools acquire automatically, so call this explicitly when you need control or want to observe a busy device."),
		deviceIDOption(),
		mcp.WithString(argMode,
			mcp.Enum(string(AccessModeControl), string(AccessModeView)),
			mcp.Description("control (default) acquires control, view joins as a read-only observer."),
		),
		mcp.WithBoolean(argTakeover,
			mcp.Description("Take the device over from the current controller. Only honored when the conflict policy is preempt."),
		),
	), mcpAcquireDeviceHandler)

	addTool(mcp.NewTool(toolReleaseDevice,
		mcp.WithDescription("Releases this MCP session's claim on a device so it can be used by others or released after the grace period."),
		deviceIDOption(),
	), mcpReleaseDeviceHandler)
}
