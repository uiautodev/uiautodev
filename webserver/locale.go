package webserver

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type localePayload struct {
	Locale string `json:"locale"`
}

func localeGetHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSONPretty(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data:    map[string]string{"locale": GetLocale()},
		}, "  ")
	}
}

func localePutHandler(config ServerConfig) echo.HandlerFunc {
	return func(c echo.Context) error {
		var payload localePayload
		if err := c.Bind(&payload); err != nil {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: "invalid locale payload"}, "  ")
		}
		locale, err := SetLocale(payload.Locale)
		if err != nil {
			return c.JSONPretty(http.StatusBadRequest, APIResponse{Code: 400, Message: err.Error()}, "  ")
		}
		if config.OnLocaleChange != nil {
			config.OnLocaleChange(locale)
		}
		return c.JSONPretty(http.StatusOK, APIResponse{
			Code:    0,
			Message: "ok",
			Data:    map[string]string{"locale": locale},
		}, "  ")
	}
}
