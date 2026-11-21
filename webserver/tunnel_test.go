package webserver

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUpstreamFromAddr(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{":33299", "http://127.0.0.1:33299"},
		{"127.0.0.1:33299", "http://127.0.0.1:33299"},
		{"0.0.0.0:8080", "http://127.0.0.1:8080"},
		{"localhost:33299", "http://localhost:33299"},
		{"[::1]:8080", "http://[::1]:8080"},
		{"http://:8443", "http://127.0.0.1:8443"},
		{"https://:8443", "https://127.0.0.1:8443"},
		{"https://example.com:8443", "https://example.com:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.want, UpstreamFromAddr(tt.addr))
		})
	}
}

func TestUpstreamScheme(t *testing.T) {
	tests := []struct {
		upstream string
		want     string
	}{
		{"http://127.0.0.1:33299", "http"},
		{"https://127.0.0.1:4333", "https"},
		{"HTTPS://127.0.0.1:4333", "https"},
		{"127.0.0.1:33299", "http"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, upstreamScheme(tt.upstream))
	}
}

func TestStartTunnelRequiresAuthtoken(t *testing.T) {
	_, err := StartTunnel(t.Context(), TunnelConfig{Upstream: "http://127.0.0.1:33299"})
	assert.ErrorContains(t, err, "authtoken")
}

type fakeNgrokError struct {
	code string
}

func (e fakeNgrokError) Error() string     { return "ngrok error: " + e.code }
func (e fakeNgrokError) ErrorCode() string { return e.code }

func TestIsInvalidAuthtokenError(t *testing.T) {
	assert.True(t, isInvalidAuthtokenError(fakeNgrokError{code: "ERR_NGROK_105"}))
	assert.True(t, isInvalidAuthtokenError(fakeNgrokError{code: "ERR_NGROK_106"}))
	assert.True(t, isInvalidAuthtokenError(fakeNgrokError{code: "ERR_NGROK_107"}))
	assert.True(t, isInvalidAuthtokenError(fmt.Errorf("wrapped: %w", fakeNgrokError{code: "ERR_NGROK_105"})))

	assert.False(t, isInvalidAuthtokenError(fakeNgrokError{code: "ERR_NGROK_6024"}))
	assert.False(t, isInvalidAuthtokenError(errors.New("boom")))
}

// TestStartTunnelInvalidAuthtokenFailsFast verifies that an invalid authtoken
// (which ngrok retries forever) does not leave the caller blocked. Offline it
// falls back to ConnectTimeout, online it should fail much sooner.
func TestStartTunnelInvalidAuthtokenFailsFast(t *testing.T) {
	start := time.Now()
	_, err := StartTunnel(t.Context(), TunnelConfig{
		Upstream:       "http://127.0.0.1:33299",
		Authtoken:      "2_invalid_token_for_test",
		ConnectTimeout: time.Second,
	})
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, 5*time.Second)
}
