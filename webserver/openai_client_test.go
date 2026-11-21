package webserver

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProxyURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string // normalized URL; empty means direct (nil)
		wantErr bool
	}{
		{"empty", "", "", false},
		{"blank", "   ", "", false},
		{"no scheme", "127.0.0.1:7890", "http://127.0.0.1:7890", false},
		{"http", "http://127.0.0.1:7890", "http://127.0.0.1:7890", false},
		{"https", "https://proxy.example.com:8080", "https://proxy.example.com:8080", false},
		{"socks5", "socks5://127.0.0.1:1080", "socks5://127.0.0.1:1080", false},
		{"socks5h", "socks5h://127.0.0.1:1080", "socks5h://127.0.0.1:1080", false},
		{"with auth", "http://user:pass@127.0.0.1:7890", "http://user:pass@127.0.0.1:7890", false}, // pragma: allowlist secret
		{"unsupported scheme", "ftp://127.0.0.1:21", "", true},
		{"missing host", "http://", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := parseProxyURL(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.want == "" {
				assert.Nil(t, u)
				return
			}
			require.NotNil(t, u)
			assert.Equal(t, tc.want, u.String())
		})
	}
}

func TestNewOpenAIHTTPClient_Proxy(t *testing.T) {
	client, err := newOpenAIHTTPClient("socks5://127.0.0.1:1080", 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, client.Timeout)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	proxyURL, err := transport.Proxy(req)
	require.NoError(t, err)
	require.NotNil(t, proxyURL)
	assert.Equal(t, "socks5://127.0.0.1:1080", proxyURL.String())

	// Empty proxy means direct connection and must not error.
	direct, err := newOpenAIHTTPClient("", 0)
	require.NoError(t, err)
	assert.NotNil(t, direct)
}

func TestNewOpenAIHTTPClient_InvalidProxy(t *testing.T) {
	_, err := newOpenAIHTTPClient("ftp://127.0.0.1:21", 0)
	require.Error(t, err)
}
