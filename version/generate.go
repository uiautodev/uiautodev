//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	// Get git commit hash
	gitCommit := getGitCommit()

	// Get build time
	buildTime := time.Now().UTC().Format(time.RFC3339)

	// Get version from git tag or use default
	version := getVersionFromGit()

	// Read the template file
	templatePath := filepath.Join(".", "version.go.tmpl")
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading template: %v\n", err)
		os.Exit(1)
	}

	// Replace placeholders
	content := string(templateContent)
	content = strings.ReplaceAll(content, "{{.Version}}", version)
	content = strings.ReplaceAll(content, "{{.GitCommit}}", gitCommit)
	content = strings.ReplaceAll(content, "{{.BuildTime}}", buildTime)

	// Write to version.go
	outputPath := filepath.Join(".", "version.go")
	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing version.go: %v\n", err)
		os.Exit(1)
	}
	// write version to version.json
	if err := writeVersionJSON(version, gitCommit, buildTime); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing version.json: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated version.go: Version=%s, GitCommit=%s, BuildTime=%s\n", version, gitCommit, buildTime)
}

func getGitCommit() string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func getVersionFromGit() string {
	// Try to get the latest tag
	cmd := exec.Command("git", "describe", "--tags", "--always", "--dirty")
	output, err := cmd.Output()
	if err != nil {
		return "0.0.0-dev"
	}
	version := strings.TrimSpace(string(output))
	// If the version starts with "v", strip it
	version = strings.TrimPrefix(version, "v")
	return version
}

func writeVersionJSON(version, gitCommit, buildTime string) error {
	versionInfo := map[string]string{
		"version":   version,
		"gitCommit": gitCommit,
		"buildTime": buildTime,
	}
	jsonData, err := json.MarshalIndent(versionInfo, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling version info: %w", err)
	}
	versionJsonPath := filepath.Join(".", "version.json")
	if err := os.WriteFile(versionJsonPath, jsonData, 0644); err != nil {
		return fmt.Errorf("writing version.json: %w", err)
	}
	return nil
}
