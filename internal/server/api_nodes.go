package server

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/archive"
	"Tefnut/internal/store"
	"Tefnut/internal/thumb"
)

type nodeDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Type        int    `json:"type"`
	PageCount   int    `json:"pageCount"`
	Rating      int    `json:"rating"`
	CoverStatus int    `json:"coverStatus"`
}

func toNodeDTO(n *store.Node) nodeDTO {
	return nodeDTO{ID: n.ID, Name: n.Name, Type: int(n.Type),
		PageCount: n.PageCount, Rating: n.Rating, CoverStatus: n.CoverStatus}
}

func parseID(c echo.Context, name string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return id, nil
}

func (s *Server) apiNodes(c echo.Context) error {
	ctx := c.Request().Context()
	q := c.QueryParam("q")
	tagID, _ := strconv.ParseInt(c.QueryParam("tag"), 10, 64)
	minRating, _ := strconv.Atoi(c.QueryParam("minRating"))

	var nodes []*store.Node
	var err error
	if q != "" || tagID > 0 || minRating > 0 {
		nodes, err = s.nodes.Search(ctx, q, tagID, minRating)
	} else {
		parent, _ := strconv.ParseInt(c.QueryParam("parent"), 10, 64)
		nodes, err = s.nodes.ListChildren(ctx, parent)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	out := make([]nodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toNodeDTO(n))
	}
	return ok(c, out)
}

type comicDetailDTO struct {
	ID               int64        `json:"id"`
	Name             string       `json:"name"`
	Author           string       `json:"author"`
	Rating           int          `json:"rating"`
	PageCount        int          `json:"pageCount"`
	LastPage         int          `json:"lastPage"`
	Tags             []*store.Tag `json:"tags"`
	ReadingDirection string       `json:"readingDirection"`
}

func (s *Server) apiComicDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	last, _ := s.progress.Get(ctx, id)
	tags, _ := s.tags.ListForNode(ctx, id)
	return ok(c, comicDetailDTO{
		ID: n.ID, Name: n.Name, Author: n.Author, Rating: n.Rating,
		PageCount: n.PageCount, LastPage: last, Tags: tags,
		ReadingDirection: n.ReadingDirection,
	})
}

func (s *Server) apiCover(c echo.Context) error {
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	p := filepath.Join(s.dataDir, "thumbs", strconv.FormatInt(id, 10)+".jpg")
	if _, statErr := filepathStat(p); statErr != nil {
		return c.Redirect(http.StatusFound, "/static/img/placeholder.svg")
	}
	return c.File(p)
}

func (s *Server) apiPage(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	pageNum, err := strconv.Atoi(c.Param("n"))
	if err != nil || pageNum < 0 {
		return fail(c, http.StatusBadRequest, fmt.Errorf("invalid page"))
	}
	cacheDir := filepath.Join(s.dataDir, "cache", strconv.FormatInt(id, 10))
	r, err := archive.Open(ctx, n.Path, cacheDir)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	defer r.Close()
	names := r.List()
	if pageNum >= len(names) {
		return fail(c, http.StatusNotFound, fmt.Errorf("page out of range"))
	}
	rc, err := r.Open(names[pageNum])
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	defer rc.Close()
	return c.Stream(http.StatusOK, contentType(names[pageNum]), rc)
}

func (s *Server) apiPageThumb(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	pageNum, err := strconv.Atoi(c.Param("n"))
	if err != nil || pageNum < 0 {
		return fail(c, http.StatusBadRequest, fmt.Errorf("invalid page"))
	}
	key := strconv.FormatInt(id, 10) + ":" + strconv.Itoa(pageNum)
	if b, ok := s.thumbs.get(key); ok {
		c.Response().Header().Set("Cache-Control", "public, max-age=86400")
		return c.Blob(http.StatusOK, "image/jpeg", b)
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	cacheDir := filepath.Join(s.dataDir, "cache", strconv.FormatInt(id, 10))
	// Each thumbnail request opens the archive fresh. For zip/cbz this only
	// reads the (small) central directory, the decode/resize dominates and is
	// inherent, the frontend lazy-loads only visible thumbs (IntersectionObserver),
	// and generated thumbs are served from the in-memory cache + browser cache
	// (Cache-Control). Acceptable for a single-user home library.
	r, err := archive.Open(ctx, n.Path, cacheDir)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	defer r.Close()
	names := r.List()
	if pageNum >= len(names) {
		return fail(c, http.StatusNotFound, fmt.Errorf("page out of range"))
	}
	rc, err := r.Open(names[pageNum])
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	defer rc.Close()
	data, err := thumb.Generate(rc, 120)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	s.thumbs.put(key, data)
	c.Response().Header().Set("Cache-Control", "public, max-age=86400")
	return c.Blob(http.StatusOK, "image/jpeg", data)
}
