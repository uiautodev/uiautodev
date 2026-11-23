package utils_test

import (
	"net"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/uiautodev/uiautodev/webserver/utils"
)

func TestShellJoin(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "simple args",
			args:     []string{"echo", "hello"},
			expected: "echo hello",
		},
		{
			name:     "args with spaces",
			args:     []string{"echo", "hello world"},
			expected: "echo 'hello world'",
		},
		{
			name:     "args with quotes",
			args:     []string{"echo", "it's"},
			expected: "echo it\\'s",
		},
		{
			name:     "args with special characters",
			args:     []string{"sh", "-c", "echo $HOME"},
			expected: "sh -c 'echo $HOME'",
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: "",
		},
		{
			name:     "single arg",
			args:     []string{"ls"},
			expected: "ls",
		},
		{
			name:     "args with backslash",
			args:     []string{"echo", "hello\\world"},
			expected: "echo hello\\\\world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.ShellJoin(tt.args)
			if result != tt.expected {
				t.Errorf("shellJoin(%v) = %q, want %q", tt.args, result, tt.expected)
			}
		})
	}
}

func TestWaitForPort(t *testing.T) {
	t.Run("port already open", func(t *testing.T) {
		// Start a listener on a random port
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to start listener: %v", err)
		}
		defer listener.Close()

		addr := listener.Addr().(*net.TCPAddr)
		port := addr.Port

		// WaitForPort should succeed immediately
		err = utils.WaitForPort("127.0.0.1", port, 2*time.Second)
		if err != nil {
			t.Errorf("WaitForPort failed for open port: %v", err)
		}
	})

	t.Run("port opens after delay", func(t *testing.T) {
		// Get a free port
		freePort, err := utils.GetFreePort()
		if err != nil {
			t.Fatalf("failed to get free port: %v", err)
		}

		// Start a server in the background after a delay
		done := make(chan struct{})
		go func() {
			time.Sleep(100 * time.Millisecond)
			listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(freePort))
			if err != nil {
				return
			}
			defer listener.Close()
			close(done)
			time.Sleep(500 * time.Millisecond)
		}()

		// WaitForPort should succeed after the port opens
		err = utils.WaitForPort("127.0.0.1", freePort, 1*time.Second)
		if err != nil {
			t.Errorf("WaitForPort failed: %v", err)
		}
		<-done
	})

	t.Run("port never opens - timeout", func(t *testing.T) {
		// Get a free port that won't be used
		port, err := utils.GetFreePort()
		if err != nil {
			t.Fatalf("failed to get free port: %v", err)
		}

		// WaitForPort should timeout
		err = utils.WaitForPort("127.0.0.1", port, 200*time.Millisecond)
		if err == nil {
			t.Error("WaitForPort should have timed out but succeeded")
		}
	})

	t.Run("invalid host", func(t *testing.T) {
		// WaitForPort should fail for invalid host
		err := utils.WaitForPort("invalid.host.local", 8080, 1*time.Second)
		if err == nil {
			t.Error("WaitForPort should fail for invalid host")
		}
	})
}

func TestFindBinary(t *testing.T) {
	t.Run("finds existing binary in PATH", func(t *testing.T) {
		result := utils.FindBinary("go")
		if result == "" {
			t.Error("FindBinary should return non-empty result for 'go'")
		}
	})

	t.Run("returns input name for non-existent binary", func(t *testing.T) {
		result := utils.FindBinary("nonexistent-binary-12345")
		expected := "nonexistent-binary-12345"
		if runtime.GOOS == "windows" {
			expected += ".exe"
		}
		if result != expected {
			t.Errorf("FindBinary() = %q, want %q", result, expected)
		}
	})

	t.Run("strips .exe suffix from input", func(t *testing.T) {
		// Use a name that definitely won't exist in the system
		result := utils.FindBinary("myapp-test-binary.exe")
		expected := "myapp-test-binary"
		if runtime.GOOS == "windows" {
			expected += ".exe"
		}
		if result != expected {
			t.Errorf("FindBinary() = %q, want %q", result, expected)
		}
	})
}
