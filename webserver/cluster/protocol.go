package cluster

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// ProtocolVersion is bumped when the tunnel handshake or stream framing
	// changes in a way that breaks compatibility.
	ProtocolVersion = 1

	// FrameHTTP marks a stream carrying an HTTP/1.1 request.
	FrameHTTP byte = 0x01
	// FrameRaw marks a stream carrying a raw TCP connection.
	FrameRaw byte = 0x02

	// ConnectPath is the master endpoint slaves dial.
	ConnectPath = "/api/v1/cluster/connect"

	SessionControl = "control"
	SessionMedia   = "media"
)

// Hello is the first message a slave sends after the WebSocket upgrade.
type Hello struct {
	SlaveID         string `json:"slave_id"`
	Name            string `json:"name"`
	NodeID          string `json:"node_id,omitempty"`
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocol_version"`
	Kind            string `json:"kind"`
}

// RawTarget describes what a raw tunnel stream should connect to.
type RawTarget struct {
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	DeviceID   string `json:"device_id,omitempty"`
	SocketAddr string `json:"socket_addr,omitempty"`
}

// Device is a device as reported by a slave, or the master's view of it.
type Device struct {
	ID          string         `json:"id"`
	Platform    string         `json:"platform"`
	Props       map[string]any `json:"props"`
	Disabled    bool           `json:"disabled"`
	SlaveID     string         `json:"slave_id,omitempty"`
	SlaveName   string         `json:"slave_name,omitempty"`
	Remote      bool           `json:"remote,omitempty"`
	SlaveOnline bool           `json:"slave_online,omitempty"`
	// Chain lists the node IDs a device passed through, ordered from the node
	// the device is attached to up to the node that reported it. Masters use it
	// to drop devices that already exist upstream when nested aggregation is
	// enabled, so a cyclic master/slave topology cannot loop forever.
	Chain []string `json:"chain,omitempty"`
}

// Snapshot is the slave's device inventory.
type Snapshot struct {
	Devices   []Device  `json:"devices"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SlaveMode describes how the master reaches a slave.
type SlaveMode string

const (
	// SlaveModePush is the classic mode: the slave dials out to the master.
	SlaveModePush SlaveMode = "push"
	// SlaveModeDirect lets the master dial the slave over HTTP(S) with Basic
	// Auth. It is intended for publicly reachable slaves.
	SlaveModeDirect SlaveMode = "direct"
)

// DirectConfig holds the master-side connection settings for direct mode.
type DirectConfig struct {
	URL         string `json:"url"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	InsecureTLS bool   `json:"insecure_tls,omitempty"`
}

// SlaveRecord is the persisted slave registration on the master.
type SlaveRecord struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Mode      SlaveMode     `json:"mode,omitempty"`
	TokenHash string        `json:"token_hash,omitempty"`
	Direct    *DirectConfig `json:"direct,omitempty"`
	Disabled  bool          `json:"disabled,omitempty"`
	// IncludeNested aggregates devices that the slave itself got from its own
	// slaves. Disabled by default to avoid duplicate devices and cycles.
	IncludeNested bool      `json:"include_nested,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (r SlaveRecord) IsDirect() bool {
	return r.Direct != nil || r.Mode == SlaveModeDirect
}

func (r SlaveRecord) DisplayMode() SlaveMode {
	if r.IsDirect() {
		return SlaveModeDirect
	}
	return SlaveModePush
}

// SlaveView is the public representation of a slave for the master UI.
type SlaveView struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Mode          SlaveMode `json:"mode"`
	URL           string    `json:"url,omitempty"`
	Username      string    `json:"username,omitempty"`
	InsecureTLS   bool      `json:"insecure_tls,omitempty"`
	Online        bool      `json:"online"`
	Disabled      bool      `json:"disabled"`
	IncludeNested bool      `json:"include_nested"`
	NodeID        string    `json:"node_id,omitempty"`
	LastSeen      time.Time `json:"last_seen"`
	Version       string    `json:"version,omitempty"`
	DeviceCount   int       `json:"device_count"`
	CreatedAt     time.Time `json:"created_at"`
}

// Store persists slave registrations on the master.
type Store interface {
	ListSlaves() ([]SlaveRecord, error)
	GetSlave(id string) (SlaveRecord, bool, error)
	SaveSlave(record SlaveRecord) error
	DeleteSlave(id string) error
}

var (
	ErrInvalidToken  = errors.New("invalid slave token")
	ErrInvalidURL    = errors.New("invalid master URL")
	ErrUnknownSlave  = errors.New("unknown slave")
	ErrSlaveOffline  = errors.New("slave is offline")
	ErrSlaveDisabled = errors.New("slave is disabled")
)

// NormalizeMasterURL validates an origin-only master URL and returns the
// WebSocket endpoint used by the slave agent.
func NormalizeMasterURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: master URL is required", ErrInvalidURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: host is required", ErrInvalidURL)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: user info is not supported", ErrInvalidURL)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("%w: only an origin is accepted, not a path", ErrInvalidURL)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: query and fragment are not supported", ErrInvalidURL)
	}
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return scheme + "://" + u.Host + ConnectPath, nil
}

// NormalizeDirectURL validates a slave base URL used by the master to reach a
// slave directly. Only an http/https origin is accepted.
func NormalizeDirectURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: slave URL is required", ErrInvalidURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: host is required", ErrInvalidURL)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: user info is not supported", ErrInvalidURL)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("%w: only an origin is accepted, not a path", ErrInvalidURL)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: query and fragment are not supported", ErrInvalidURL)
	}
	return u.Scheme + "://" + u.Host, nil
}

// NormalizeSlaveAddr makes the slave's local HTTP listener loopback-only. An
// empty address keeps headless mode; wildcard binds are rewritten to 127.0.0.1.
func NormalizeSlaveAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid slave listen address %q: %w", addr, err)
	}
	if port == "" {
		return "", fmt.Errorf("invalid slave listen address %q: missing port", addr)
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

// GenerateSlaveID returns a 4-character lowercase hex slave ID.
func GenerateSlaveID() (string, error) {
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GenerateSecret returns a 20-character hex secret.
func GenerateSecret() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// NewToken builds the user-facing slave token.
func NewToken(slaveID, secret string) string {
	return "u-" + slaveID + "-" + secret
}

// ParseToken splits a slave token into its ID and secret.
func ParseToken(token string) (slaveID, secret string, err error) {
	parts := strings.SplitN(strings.TrimSpace(token), "-", 3)
	if len(parts) != 3 || (parts[0] != "u" && parts[0] != "uia") || parts[1] == "" || parts[2] == "" {
		return "", "", ErrInvalidToken
	}
	return parts[1], parts[2], nil
}

// HashSecret returns the persisted token hash.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// DefaultSlaveName returns the machine hostname, falling back to the slave ID.
func DefaultSlaveName(slaveID string) string {
	if host, err := os.Hostname(); err == nil {
		if host = strings.TrimSpace(host); host != "" {
			return host
		}
	}
	return slaveID
}

// ParseGlobalDeviceID splits a remote device ID into its slave and local IDs.
func ParseGlobalDeviceID(id string) (slaveID, localID string, ok bool) {
	if !strings.HasPrefix(id, "r-") {
		return "", "", false
	}
	rest := strings.TrimPrefix(id, "r-")
	idx := strings.IndexByte(rest, '-')
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

// GlobalDeviceID builds the master-wide device ID.
func GlobalDeviceID(slaveID, localID string) string {
	return "r-" + slaveID + "-" + localID
}
