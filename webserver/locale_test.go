package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupLocaleConfig(t *testing.T) {
	t.Helper()
	SetConfigFile(filepath.Join(t.TempDir(), configFileName))
	t.Cleanup(func() { SetConfigFile("") })
}

func TestLocalePersists(t *testing.T) {
	setupLocaleConfig(t)
	assert.Equal(t, LocaleChinese, GetLocale())

	locale, err := SetLocale("en-US")
	require.NoError(t, err)
	assert.Equal(t, LocaleEnglish, locale)
	assert.Equal(t, LocaleEnglish, GetLocale())

	locale, err = SetLocale("zh-Hans")
	require.NoError(t, err)
	assert.Equal(t, LocaleChinese, locale)
	assert.Equal(t, LocaleChinese, GetLocale())

	_, err = SetLocale("fr")
	require.Error(t, err)
}

func TestLocalePutHandlerRebuildsMenu(t *testing.T) {
	setupLocaleConfig(t)
	var changed string
	config := ServerConfig{OnLocaleChange: func(locale string) { changed = locale }}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/locale", strings.NewReader(`{"locale":"en"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	require.NoError(t, localePutHandler(config)(c))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, LocaleEnglish, changed)
	assert.Equal(t, LocaleEnglish, GetLocale())
}

func TestLocaleGetHandler(t *testing.T) {
	setupLocaleConfig(t)
	_, err := SetLocale("en")
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locale", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	require.NoError(t, localeGetHandler()(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var payload struct {
		Locale string `json:"locale"`
	}
	require.NoError(t, json.Unmarshal(envelopeData(t, rec), &payload))
	assert.Equal(t, LocaleEnglish, payload.Locale)
}
