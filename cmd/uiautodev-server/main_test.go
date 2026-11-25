package main

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultAddrListensOnAllInterfaces guards the default -addr: an empty
// host binds every interface, so phones on the LAN can reach the dashboard.
func TestDefaultAddrListensOnAllInterfaces(t *testing.T) {
	host, port, err := net.SplitHostPort(defaultAddr)
	require.NoError(t, err)
	assert.Empty(t, host, "default -addr must not restrict the host")
	assert.Equal(t, "33299", port)
}
