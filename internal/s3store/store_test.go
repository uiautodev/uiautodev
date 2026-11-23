package s3store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const (
	testAccessKey = "file-key"
	testSecretKey = "file-secret" // pragma: allowlist secret
	envAccessKey  = "env-key"
	envSecretKey  = "env-secret" // pragma: allowlist secret
)

func rcloneConfig(endpoint string) string {
	return fmt.Sprintf(`[r2]
type = s3
provider = Cloudflare
access_key_id = %s
secret_access_key = %s
endpoint = %s
`, testAccessKey, testSecretKey, endpoint)
}

func writeRcloneConfig(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "rclone")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rclone.conf"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFromEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("S3_ENDPOINT", "https://env.example.com")
	t.Setenv("S3_ACCESS_KEY_ID", envAccessKey)
	t.Setenv("S3_SECRET_ACCESS_KEY", envSecretKey) // pragma: allowlist secret

	settings, err := Load("", "")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Endpoint != "https://env.example.com" {
		t.Errorf("endpoint = %q", settings.Endpoint)
	}
	if settings.AccessKeyID != envAccessKey || settings.SecretAccessKey != envSecretKey { // pragma: allowlist secret
		t.Errorf("credentials = %q / %q", settings.AccessKeyID, settings.SecretAccessKey)
	}
	if settings.Region != "auto" {
		t.Errorf("region = %q, want auto", settings.Region)
	}
}

func TestLoadFallsBackToRclone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("S3_ENDPOINT", "")
	t.Setenv("S3_ACCESS_KEY_ID", "")
	t.Setenv("S3_SECRET_ACCESS_KEY", "")
	t.Setenv("R2_ACCESS_KEY_ID", "")
	t.Setenv("R2_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	writeRcloneConfig(t, home, rcloneConfig("https://file.example.com"))

	settings, err := Load("", "")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Endpoint != "https://file.example.com" {
		t.Errorf("endpoint = %q", settings.Endpoint)
	}
	if settings.AccessKeyID != testAccessKey || settings.SecretAccessKey != testSecretKey { // pragma: allowlist secret
		t.Errorf("credentials = %q / %q", settings.AccessKeyID, settings.SecretAccessKey)
	}
}

func TestLoadEnvOverridesRclone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("R2_ACCESS_KEY_ID", envAccessKey)
	t.Setenv("R2_SECRET_ACCESS_KEY", envSecretKey) // pragma: allowlist secret

	writeRcloneConfig(t, home, rcloneConfig("https://file.example.com"))

	settings, err := Load("", "")
	if err != nil {
		t.Fatal(err)
	}
	if settings.AccessKeyID != envAccessKey || settings.SecretAccessKey != envSecretKey { // pragma: allowlist secret
		t.Errorf("credentials = %q / %q, want env values", settings.AccessKeyID, settings.SecretAccessKey)
	}
	if settings.Endpoint != "https://file.example.com" {
		t.Errorf("endpoint = %q, want rclone value", settings.Endpoint)
	}
}

func TestLoadMissingEndpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("S3_ENDPOINT", "")
	t.Setenv("S3_ACCESS_KEY_ID", envAccessKey)
	t.Setenv("S3_SECRET_ACCESS_KEY", envSecretKey) // pragma: allowlist secret

	if _, err := Load("", ""); err == nil {
		t.Error("Load should fail without an endpoint")
	}
}

func TestLoadMissingCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("S3_ENDPOINT", "https://env.example.com")

	if _, err := Load("", ""); err == nil {
		t.Error("Load should fail without credentials")
	}
}
