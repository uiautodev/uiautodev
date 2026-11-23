package utils

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kballard/go-shellquote"
)

func GetFreePort() (port int, err error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

func ShellJoin(args []string) string {
	return shellquote.Join(args...)
}

// WaitForPort waits for the specified port to be open within the given timeout
func WaitForPort(host string, port int, timeout time.Duration) error {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	startTime := time.Now()
	for time.Since(startTime) < timeout {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP(host), Port: port}, Err: fmt.Errorf("timeout waiting for port")}
}

func getExeDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	exeDir := filepath.Dir(exePath)
	// Try to resolve symlinks to get the real path
	if realPath, err := filepath.EvalSymlinks(exePath); err == nil {
		exeDir = filepath.Dir(realPath)
	}
	return exeDir, nil
}

// IsExists checks if a path exists.
func IsExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// FindBinary returns the path to the specified binary by checking the executable directory first,
// then the tools subdirectory, and finally the PATH environment variable.
func FindBinary(name string) string {
	binName := strings.TrimSuffix(name, ".exe")
	if runtime.GOOS == "windows" {
		binName = binName + ".exe"
	}
	exeDir, err := getExeDir()
	if err == nil {
		slog.Debug("exeDir", "path", exeDir)
		// Check in executable directory
		binPath := filepath.Join(exeDir, binName)
		if IsExists(binPath) {
			slog.Debug("Found binary", "path", binPath)
			return binPath
		}
		// Check in tools subdirectory
		binPath = filepath.Join(exeDir, "tools", binName)
		if IsExists(binPath) {
			slog.Debug("Found binary", "path", binPath)
			return binPath
		}
	}
	// try PATH environment variable
	binPath, err := exec.LookPath(binName)
	if err == nil {
		slog.Debug("Found via LookPath", "path", binPath)
		return binPath
	}
	slog.Warn("Binary not found", "error", err)
	return binName
}
