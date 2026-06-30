package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/store"
)

func validTagName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("tag name required")
	}
	if utf8.RuneCountInString(name) > 64 {
		return "", errors.New("tag name too long")
	}
	return name, nil
}

func (s *Server) apiListTags(c echo.Context) error {
	tags, err := s.tags.List(c.Request().Context())
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, tags)
}

func (s *Server) apiCreateTag(c echo.Context) error {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	name, err := validTagName(body.Name)
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	tag, err := s.tags.Upsert(c.Request().Context(), name)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, tag)
}

func (s *Server) apiRenameTag(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("invalid id"))
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	name, err := validTagName(body.Name)
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	if err := s.tags.Rename(c.Request().Context(), id, name); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return fail(c, http.StatusConflict, err)
		}
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, map[string]any{"id": id, "name": name})
}

func (s *Server) apiDeleteTag(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("invalid id"))
	}
	if err := s.tags.Delete(c.Request().Context(), id); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}

func (s *Server) pageTags(c echo.Context) error {
	tags, err := s.tags.List(c.Request().Context())
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return render(c, "tags.html", map[string]any{"Tags": tags})
}
