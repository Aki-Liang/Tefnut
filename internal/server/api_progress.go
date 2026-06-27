package server

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/store"
)

func (s *Server) apiSetProgress(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	var body struct {
		Page int `json:"page"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if body.Page < 0 || (n.PageCount > 0 && body.Page >= n.PageCount) {
		return fail(c, http.StatusBadRequest, errors.New("page out of range"))
	}
	if err := s.progress.Set(ctx, id, body.Page); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}
