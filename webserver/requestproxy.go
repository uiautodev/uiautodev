package webserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
)

func isHostAllowed(allowedHosts []string, targetHost string) bool {
	if len(allowedHosts) == 0 {
		return false
	}
	hostOnly := targetHost
	if idx := strings.Index(targetHost, "://"); idx != -1 {
		hostOnly = targetHost[idx+3:]
	}
	for _, pattern := range allowedHosts {
		patternHasScheme := strings.Contains(pattern, "://")
		if patternHasScheme {
			if ok, _ := path.Match(pattern, targetHost); ok {
				return true
			}
		} else {
			if ok, _ := path.Match(pattern, hostOnly); ok {
				return true
			}
		}
	}
	return false
}

func proxyRequestHandler(c echo.Context) error {
	targetHost := c.Request().Header.Get("X-Target-Host")
	if targetHost == "" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "missing X-Target-Host header",
		}, "  ")
	}

	targetURL, err := url.Parse(targetHost)
	if err != nil || targetURL.Host == "" || targetURL.Scheme == "" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid X-Target-Host (expect origin like https://example.com): " + targetHost,
		}, "  ")
	}
	if targetURL.Path != "" || targetURL.RawQuery != "" || targetURL.Fragment != "" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "invalid X-Target-Host (must not include path/query/fragment): " + targetHost,
		}, "  ")
	}
	if targetURL.Scheme != "http" && targetURL.Scheme != "https" {
		return c.JSONPretty(http.StatusBadRequest, APIResponse{
			Code:    400,
			Message: "unsupported X-Target-Host scheme: " + targetURL.Scheme,
		}, "  ")
	}
	targetHost = targetURL.Scheme + "://" + targetURL.Host

	pluginID := c.Request().Header.Get("X-Plugin-ID")
	if pluginID == "" {
		return c.JSONPretty(http.StatusForbidden, APIResponse{
			Code:    403,
			Message: "missing X-Plugin-ID header",
		}, "  ")
	}

	plugin, ok := GetPluginManager().GetPlugin(pluginID)
	if !ok {
		return c.JSONPretty(http.StatusForbidden, APIResponse{
			Code:    403,
			Message: "plugin not found",
		}, "  ")
	}

	if !isHostAllowed(plugin.Manifest.AllowedHosts, targetHost) {
		return c.JSONPretty(http.StatusForbidden, APIResponse{
			Code:    403,
			Message: "target host not allowed",
		}, "  ")
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = targetURL.Scheme
			pr.Out.URL.Host = targetURL.Host

			// Strip control/sensitive headers before proxying.
			pr.Out.Header.Del("X-Target-Host")
			pr.Out.Header.Del("X-Plugin-ID")
			pr.Out.Header.Del("Cookie")
			wildcardPath := c.Param("*")
			pr.Out.URL.Path = path.Clean("/" + wildcardPath)

			if c.QueryString() != "" {
				pr.Out.URL.RawQuery = c.QueryString()
			}

			pr.Out.Host = targetURL.Host
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("proxy request failed", "target", targetHost, "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(APIResponse{
				Code:    502,
				Message: "proxy request failed: " + err.Error(),
			})
		},
	}

	proxy.ServeHTTP(c.Response(), c.Request())
	return nil
}
