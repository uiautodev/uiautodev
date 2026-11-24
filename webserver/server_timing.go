package webserver

import (
	"fmt"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
)

// serverTimingHeader carries the request timings that browser DevTools and the
// dashboard can read through the Server Timing API.
const serverTimingHeader = "Server-Timing"

// serverTiming reports the total server-side processing time of a request as a
// single `app` metric, e.g. `app;desc="total";dur=12.3`.
//
// It is registered as the outermost middleware so that rejections from auth,
// recovered panics and 404s are measured too. The duration is measured until
// the first response byte is written, so streaming and WebSocket endpoints
// report their time to first byte rather than the whole stream lifetime.
//
// Per-stage metrics (device calls, proxy hops, ...) can be appended to the same
// header later as extra entries without changing the protocol.
func serverTiming() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			c.Response().Before(func() {
				durMillis := float64(time.Since(start).Microseconds()) / 1000
				c.Response().Header().Set(serverTimingHeader,
					fmt.Sprintf(`app;desc=%q;dur=%s`, "total", strconv.FormatFloat(durMillis, 'f', 1, 64)))
			})
			return next(c)
		}
	}
}
