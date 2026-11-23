package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNativeLabelsSettings(t *testing.T) {
	assert.Equal(t, "Settings...", nativeLabelsFor("en-US").Settings)
	assert.Equal(t, "设置...", nativeLabelsFor("zh-CN").Settings)
}

func TestNewAppAPIToken(t *testing.T) {
	first := NewApp()
	second := NewApp()

	assert.NotEmpty(t, first.GetAPIToken())
	assert.NotEqual(t, first.GetAPIToken(), second.GetAPIToken())
}

func TestFilterPattern(t *testing.T) {
	assert.Equal(t, "*.zip", filterPattern([]string{"zip"}))
	assert.Equal(t, "*.mp4;*.zip", filterPattern([]string{".mp4", "zip"}))
}

func TestFilterDisplayName(t *testing.T) {
	assert.Equal(t, "MP4, ZIP Files", filterDisplayName([]string{".mp4", "zip"}))
}
