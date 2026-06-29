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

type metaReq struct {
	Author           *string `json:"author"`
	Rating           *int    `json:"rating"`
	ReadingDirection *string `json:"readingDirection"`
}

func (s *Server) apiUpdateMeta(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	var body metaReq
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	if body.Rating != nil && (*body.Rating < 0 || *body.Rating > 5) {
		return fail(c, http.StatusBadRequest, errors.New("rating must be 0..5"))
	}
	if body.ReadingDirection != nil {
		dir := *body.ReadingDirection
		if dir != "ltr" && dir != "rtl" {
			return fail(c, http.StatusBadRequest, errors.New("readingDirection must be ltr or rtl"))
		}
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	author := n.Author
	rating := n.Rating
	if body.Author != nil {
		author = *body.Author
	}
	if body.Rating != nil {
		rating = *body.Rating
	}
	if err := s.nodes.UpdateMeta(ctx, id, author, rating); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if body.ReadingDirection != nil {
		if err := s.nodes.UpdateReadingDirection(ctx, id, *body.ReadingDirection); err != nil {
			return fail(c, http.StatusInternalServerError, err)
		}
	}
	return ok(c, map[string]any{"author": author, "rating": rating})
}

type addTagReq struct {
	Name string `json:"name"`
}

func (s *Server) apiAddTag(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	var body addTagReq
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return fail(c, http.StatusBadRequest, errors.New("tag name required"))
	}
	if utf8.RuneCountInString(name) > 64 {
		return fail(c, http.StatusBadRequest, errors.New("tag name too long"))
	}
	tag, err := s.tags.Upsert(ctx, name)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if err := s.tags.AddToNode(ctx, id, tag.ID); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, tag)
}

func (s *Server) apiRemoveTag(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	tagID, err := strconv.ParseInt(c.Param("tagId"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("invalid tagId"))
	}
	if err := s.tags.RemoveFromNode(ctx, id, tagID); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}
