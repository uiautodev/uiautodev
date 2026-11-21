package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mitCopyrightNotice is the copyright line shared by build/config.yml and the
// packaging metadata generated from it.
const mitCopyrightNotice = "Licensed under the MIT License."

// TestLicenseMetadata guards the MIT migration: the generated Info.plist and
// info.json are rewritten from build/config.yml by update:build-assets, so a
// stale config would quietly restore the old dual-license wording.
func TestLicenseMetadata(t *testing.T) {
	license, err := os.ReadFile("LICENSE")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(license), "MIT License"), "LICENSE must start with the MIT template")
	assert.Contains(t, string(license), "Permission is hereby granted, free of charge")
	assert.NotContains(t, strings.ToLower(string(license)), "commercial")

	readme, err := os.ReadFile("README.md")
	require.NoError(t, err)
	assert.Contains(t, string(readme), "MIT License")
	assert.NotContains(t, string(readme), "双授权")

	var pkg struct {
		License string `json:"license"`
	}
	data, err := os.ReadFile("frontend/package.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &pkg))
	assert.Equal(t, "MIT", pkg.License)

	layout, err := os.ReadFile("frontend/src/routes/+layout.svelte")
	require.NoError(t, err)
	assert.Contains(t, string(layout), "&copy; 2026 UiautoDev")
	assert.NotContains(t, string(layout), "All rights reserved")

	for _, path := range []string{
		"build/config.yml",
		"build/darwin/Info.plist",
		"build/darwin/Info.dev.plist",
		"build/windows/info.json",
	} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(data), mitCopyrightNotice, "%s must carry the MIT copyright notice", path)
		assert.NotContains(t, string(data), "Free for personal", "%s still advertises the old dual license", path)
	}
}
