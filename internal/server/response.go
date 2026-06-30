package server

import (
	"log"

	"github.com/labstack/echo/v4"
)

func ok(c echo.Context, data any) error {
	return c.JSON(200, map[string]any{"code": 0, "message": "success", "data": data})
}

// fail writes a JSON error. For 5xx the real error is logged server-side and a
// generic message is returned so internal details (paths, SQL) never reach the
// client. For 4xx the (caller-authored, user-safe) message is returned as-is.
func fail(c echo.Context, status int, err error) error {
	msg := err.Error()
	if status >= 500 {
		log.Printf("server: %s %s -> %d: %v", c.Request().Method, c.Request().URL.Path, status, err)
		msg = "internal error"
	}
	return c.JSON(status, map[string]any{"code": -1, "message": msg})
}
