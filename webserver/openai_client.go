package webserver

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// supportedProxySchemes lists the proxy protocols accepted in the OpenAI
// provider config. All of them are natively handled by net/http's transport.
var supportedProxySchemes = map[string]bool{
	"http":    true,
	"https":   true,
	"socks5":  true,
	"socks5h": true,
}

// parseProxyURL normalizes and validates a proxy setting. An empty value means
// no proxy (direct connection) and returns a nil URL. A value without a scheme
// defaults to http://. Only http, https, socks5 and socks5h are accepted.
func parseProxyURL(proxy string) (*url.URL, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return nil, nil
	}
	if !strings.Contains(proxy, "://") {
		proxy = "http://" + proxy
	}
	u, err := url.Parse(proxy)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	if !supportedProxySchemes[strings.ToLower(u.Scheme)] {
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("proxy host is empty")
	}
	return u, nil
}

// newOpenAIHTTPClient builds an HTTP client routed through the configured
// proxy. An empty proxy falls back to the default transport behavior. A zero
// timeout disables the client timeout (used for streaming responses).
func newOpenAIHTTPClient(proxy string, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	u, err := parseProxyURL(proxy)
	if err != nil {
		return nil, err
	}
	if u != nil {
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}
