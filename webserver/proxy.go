package webserver

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func isLocalhost(r *http.Request) bool {
	remoteHost := r.RemoteAddr
	if idx := strings.LastIndex(remoteHost, ":"); idx != -1 {
		remoteHost = remoteHost[:idx]
	}
	// If the host includes a zone (IPv6 like "fe80::1%lo0"), strip it.
	if pct := strings.Index(remoteHost, "%"); pct != -1 {
		remoteHost = remoteHost[:pct]
	}
	if remoteHost != "127.0.0.1" && remoteHost != "::1" && remoteHost != "[::1]" {
		return false
	}
	return true
}

// /proxy/{scheme}/{host}/{path...}
func handleProxy(w http.ResponseWriter, r *http.Request) {
	// Only allow requests from localhost to prevent open proxy abuse.
	// r.RemoteAddr is in the form "IP:port". Extract the IP and check
	// whether it is a loopback address (127.0.0.1 or ::1).
	if !isLocalhost(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/proxy/"), "/", 3)
	if len(parts) < 2 {
		http.Error(w, "usage: /proxy/{http|https}/{host}/{path}", 400)
		return
	}

	scheme := parts[0] // http / https
	host := parts[1]   // example.com / 1.2.3.4:8080
	path := ""
	if len(parts) == 3 {
		path = parts[2] // 剩余路径
	}

	targetURL := &url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/" + path,
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		pr.Out.URL.Scheme = scheme
		pr.Out.URL.Host = host
		pr.Out.URL.Path = "/" + path
		pr.Out.Host = host
		pr.Out.URL.RawQuery = pr.In.URL.RawQuery
		pr.SetXForwarded()
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		slog.Debug("Proxy:", "path", r.URL.Path, "target", targetURL)
		return nil
	}

	proxy.ServeHTTP(w, r)
}
