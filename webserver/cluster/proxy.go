package cluster

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// DeviceRouteMiddleware routes requests for remote devices to their slave. It
// must run after Echo has matched the route so c.Param("device_id") is set.
func (m *Master) DeviceRouteMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			deviceID := c.Param("device_id")
			if deviceID == "" {
				return next(c)
			}
			slaveID, localID, ok := ParseGlobalDeviceID(deviceID)
			if !ok {
				return next(c)
			}
			path := c.Request().URL.Path
			switch {
			case strings.HasSuffix(path, "/meta"):
				return next(c)
			case path == "/api/v1/devices/"+deviceID && c.Request().Method == http.MethodGet:
				// Let the master's handler synthesize the response with the
				// global device ID.
				return next(c)
			case strings.HasSuffix(path, "/connect"):
				switch c.Request().Method {
				case http.MethodGet:
					return m.handleRemoteConnect(c, slaveID, localID)
				case http.MethodDelete:
					return m.handleRemoteDisconnect(c, slaveID, localID)
				default:
					return writeAPI(c, http.StatusMethodNotAllowed, 405, "method not allowed", nil)
				}
			default:
				return m.proxy(c, slaveID, localID, deviceID)
			}
		}
	}
}

func (m *Master) proxy(c echo.Context, slaveID, localID, globalID string) error {
	kind := SessionControl
	path := c.Request().URL.Path
	if strings.HasPrefix(path, "/stream/") || strings.HasPrefix(path, "/proxy/devices/") {
		kind = SessionMedia
	}
	if !m.slaveOnline(slaveID) {
		return writeAPI(c, http.StatusBadGateway, 502, "slave is offline", nil)
	}
	targetPath := strings.Replace(path, "/"+globalID, "/"+localID, 1)

	base, direct := m.slaveBaseURL(slaveID)
	var transport http.RoundTripper = m.transport(slaveID, kind)
	var target *url.URL
	if direct != nil {
		parsed, err := url.Parse(base)
		if err != nil {
			return writeAPI(c, http.StatusBadGateway, 502, "invalid slave URL", nil)
		}
		target = parsed
		if client := m.newClient(slaveID, kind); client.Transport != nil {
			transport = client.Transport
		}
	}

	proxy := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			if direct != nil {
				pr.Out.URL.Scheme = target.Scheme
				pr.Out.URL.Host = target.Host
				pr.Out.Host = target.Host
			} else {
				pr.Out.URL.Scheme = "http"
				pr.Out.URL.Host = "slave"
				pr.Out.Host = "slave"
			}
			pr.Out.URL.Path = targetPath
			pr.Out.URL.RawPath = ""
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("X-Real-IP")
			pr.Out.Header.Del("X-Forwarded-For")
			if direct != nil {
				pr.Out.SetBasicAuth(direct.Username, direct.Password)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if direct != nil {
				resp.Header.Del("Set-Cookie")
				resp.Header.Del("WWW-Authenticate")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeJSON(w, http.StatusBadGateway, 502, "slave request failed: "+err.Error())
		},
	}
	proxy.ServeHTTP(c.Response(), c.Request())
	return nil
}

func writeJSON(w http.ResponseWriter, status, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    code,
		"message": message,
	})
}
