package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uiautodev/uiautodev/webserver/device"

	"github.com/labstack/echo/v4"
)

// ProxyLogger handles logging of proxy data to a file using a channel
type ProxyLogger struct {
	enabled bool
	file    *os.File
	ch      chan string
	wg      sync.WaitGroup
	mu      sync.Mutex
}

var proxyLogger *ProxyLogger

// InitProxyLogger initializes the proxy logger with the given log file path
func InitProxyLogger(logFilePath string) error {
	// Close existing logger if any
	if proxyLogger != nil {
		proxyLogger.Close()
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file in append mode
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open proxy log file: %w", err)
	}

	proxyLogger = &ProxyLogger{
		enabled: true,
		file:    file,
		ch:      make(chan string, 1000),
	}

	// Start background writer goroutine
	proxyLogger.wg.Add(1)
	go proxyLogger.writer()

	return nil
}

// writer is a background goroutine that writes log entries to the file
func (pl *ProxyLogger) writer() {
	defer pl.wg.Done()
	for entry := range pl.ch {
		pl.file.WriteString(entry)
	}
}

// Enable turns on proxy logging
func (pl *ProxyLogger) Enable() {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.enabled = true
}

// Disable turns off proxy logging
func (pl *ProxyLogger) Disable() {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.enabled = false
}

// IsEnabled returns whether proxy logging is enabled
func (pl *ProxyLogger) IsEnabled() bool {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.enabled
}

// Log writes a log entry to the channel
func (pl *ProxyLogger) Log(entry string) {
	if !pl.IsEnabled() {
		return
	}
	select {
	case pl.ch <- entry:
	default:
		// Channel full, drop the log entry
	}
}

// Close closes the proxy logger
func (pl *ProxyLogger) Close() {
	close(pl.ch)
	pl.wg.Wait()
	pl.file.Close()
}

// CloseProxyLogger closes and cleans up the proxy logger
func CloseProxyLogger() {
	if proxyLogger != nil {
		proxyLogger.Close()
		proxyLogger = nil
	}
}

// DumpConn wraps a net.Conn and logs all Read/Write operations
type DumpConn struct {
	net.Conn
	requestID  string
	direction  string // "REQUEST" or "RESPONSE"
	writeBuf   []byte
	readBuf    []byte
	writeMutex sync.Mutex
	readMutex  sync.Mutex
	closed     bool
	closeMutex sync.Mutex
}

// NewDumpConn creates a new DumpConn with logging enabled
func NewDumpConn(conn net.Conn, requestID, direction string) *DumpConn {
	return &DumpConn{
		Conn:      conn,
		requestID: requestID,
		direction: direction,
	}
}

func (d *DumpConn) Read(b []byte) (n int, err error) {
	n, err = d.Conn.Read(b)
	if err == nil && n > 0 && proxyLogger != nil && proxyLogger.IsEnabled() {
		d.readMutex.Lock()
		d.readBuf = append(d.readBuf, b[:n]...)
		d.readMutex.Unlock()
	}
	return n, err
}

func (d *DumpConn) Write(b []byte) (n int, err error) {
	n, err = d.Conn.Write(b)
	if err == nil && n > 0 && proxyLogger != nil && proxyLogger.IsEnabled() {
		d.writeMutex.Lock()
		d.writeBuf = append(d.writeBuf, b[:n]...)
		d.writeMutex.Unlock()
	}
	return n, err
}

func (d *DumpConn) Close() error {
	d.closeMutex.Lock()
	defer d.closeMutex.Unlock()

	if d.closed {
		return d.Conn.Close()
	}
	d.closed = true

	// Log all buffered data when connection closes
	if proxyLogger != nil && proxyLogger.IsEnabled() {
		timestamp := time.Now().Format("2006-01-02 15:04:05 -07:00")
		sessionID := time.Now().UnixNano()

		var entry string
		if len(d.writeBuf) > 0 && len(d.readBuf) > 0 {
			// Log both request and response
			entry = fmt.Sprintf("\n> [%s] sid=%d %s\n%s\n%s\n%s\n< END\n",
				timestamp,
				sessionID,
				d.requestID,
				strings.TrimRight(string(d.writeBuf), "\n"),
				strings.Repeat("-", 26),
				strings.TrimRight(string(d.readBuf), "\n"),
			)
		} else if len(d.writeBuf) > 0 {
			// Only request
			entry = fmt.Sprintf("\n> [%s] sid=%d %s\n%s\n< END\n",
				timestamp,
				sessionID,
				d.requestID,
				strings.TrimRight(string(d.writeBuf), "\n"),
			)
		} else if len(d.readBuf) > 0 {
			// Only response
			entry = fmt.Sprintf("\n> [%s] sid=%d %s\n%s\n< END\n",
				timestamp,
				sessionID,
				d.requestID,
				strings.TrimRight(string(d.readBuf), "\n"),
			)
		}

		if entry != "" {
			proxyLogger.Log(entry)
		}
	}

	return d.Conn.Close()
}

// newDeviceTransport creates an HTTP transport that routes requests through a device socket connection.
func newDeviceTransport(dev device.Device, socketAddr string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dev.Connect(socketAddr)
			if err != nil {
				return nil, err
			}
			// Create a unique request ID based on timestamp
			requestID := socketAddr
			return NewDumpConn(conn, requestID, "REQUEST"), nil
		},
		DisableKeepAlives: true,
	}
}

// deviceProxyHandler proxies HTTP requests to a device socket connection using httputil.ReverseProxy.
// The URL pattern is: /proxy/devices/:device_id/:socket_addr/*
// For example: /proxy/devices/a-123456/tcp:8100/status
func deviceProxyHandler(c echo.Context) error {
	deviceId := c.Param("device_id")
	socketAddr := c.Param("socket_addr")

	dev, err := device.NewDevice(deviceId)
	if err != nil {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid device: " + err.Error(),
		}, "  ")
	}

	// Create a custom transport that routes through the device socket
	transport := newDeviceTransport(dev, socketAddr)

	// Create a ReverseProxy with a custom Director
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Update the request to point to localhost
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "localhost"

			// Update the path to use the wildcard parameter
			wildcardPath := c.Param("*")
			if wildcardPath != "" {
				pr.Out.URL.Path = path.Clean("/" + wildcardPath)
			}

			// Preserve query string
			if c.QueryString() != "" {
				pr.Out.URL.RawQuery = c.QueryString()
			}

			// Disable gzip compression for simpler proxying
			pr.Out.Header.Set("Accept-Encoding", "identity")
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("Device proxy request failed", "device", deviceId, "socket", socketAddr, "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(APIResponse{
				Code:    502,
				Message: "device proxy request failed: " + err.Error(),
			})
		},
	}

	// Serve the request through the proxy
	proxy.ServeHTTP(c.Response(), c.Request())
	return nil
}
