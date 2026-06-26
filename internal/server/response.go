package server

import "github.com/labstack/echo/v4"

func ok(c echo.Context, data any) error {
	return c.JSON(200, map[string]any{"code": 0, "message": "success", "data": data})
}

func fail(c echo.Context, status int, err error) error {
	return c.JSON(status, map[string]any{"code": -1, "message": err.Error()})
}
