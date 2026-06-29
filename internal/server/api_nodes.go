package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/store"
	"Tefnut/internal/thumb"
)

var errPageRange = errors.New("page out of range")

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
	limit, _ := strconv.Atoi(c.QueryParam("limit")) // 0 => default cap
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	if offset < 0 {
		offset = 0
	}

	var nodes []*store.Node
	var err error
	if q != "" || tagID > 0 || minRating > 0 {
		nodes, err = s.nodes.Search(ctx, q, tagID, minRating, limit, offset)
	} else {
		parent, _ := strconv.ParseInt(c.QueryParam("parent"), 10, 64)
		nodes, err = s.nodes.ListChildren(ctx, parent, limit, offset)
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
	DisplayMode      string       `json:"displayMode"`
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
		DisplayMode:      n.DisplayMode,
	})
}

func (s *Server) apiCover(c echo.Context) error {
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	p := filepath.Join(s.dataDir, "thumbs", strconv.FormatInt(id, 10)+".jpg")
	info, statErr := filepathStat(p)
	if statErr != nil {
		return c.Redirect(http.StatusFound, "/static/img/placeholder.svg")
	}
	etag := fmt.Sprintf(`"cover-%d-%d"`, id, info.ModTime().Unix())
	c.Response().Header().Set("ETag", etag)
	c.Response().Header().Set("Cache-Control", "private, max-age=86400")
	if match := c.Request().Header.Get("If-None-Match"); match == etag {
		return c.NoContent(http.StatusNotModified)
	}
	return c.File(p)
}

// openPage acquires a (cached) archive reader for node n, returns the entry
// reader for pageNum, its name, and a release func that frees the archive
// reader. Callers MUST defer release() AND close the returned entry reader.
func (s *Server) openPage(ctx context.Context, n *store.Node, pageNum int) (io.ReadCloser, string, func(), error) {
	cacheDir := filepath.Join(s.dataDir, "cache", strconv.FormatInt(n.ID, 10))
	key := strconv.FormatInt(n.ID, 10)
	r, release, err := s.readers.Acquire(ctx, key, n.Path, n.MTime, cacheDir)
	if err != nil {
		return nil, "", nil, err
	}
	names := r.List()
	if pageNum < 0 || pageNum >= len(names) {
		release()
		return nil, "", nil, errPageRange
	}
	rc, err := r.Open(names[pageNum])
	if err != nil {
		release()
		return nil, "", nil, err
	}
	return rc, names[pageNum], release, nil
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
	etag := fmt.Sprintf(`"%d-%d-%d"`, n.ID, n.MTime, pageNum)
	c.Response().Header().Set("ETag", etag)
	c.Response().Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if match := c.Request().Header.Get("If-None-Match"); match == etag {
		return c.NoContent(http.StatusNotModified)
	}
	rc, name, release, err := s.openPage(ctx, n, pageNum)
	if err != nil {
		if errors.Is(err, errPageRange) {
			return fail(c, http.StatusNotFound, err)
		}
		return fail(c, http.StatusInternalServerError, err)
	}
	defer release()
	defer rc.Close()
	return c.Stream(http.StatusOK, contentType(name), rc)
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
	rc, _, release, err := s.openPage(ctx, n, pageNum)
	if err != nil {
		if errors.Is(err, errPageRange) {
			return fail(c, http.StatusNotFound, err)
		}
		return fail(c, http.StatusInternalServerError, err)
	}
	defer release()
	defer rc.Close()
	s.decodeSem <- struct{}{}
	data, err := thumb.Generate(rc, s.pageThumbWidth)
	<-s.decodeSem
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if err := s.thumbs.put(key, data); err != nil {
		log.Printf("server: persist thumb %s: %v", key, err)
	}
	c.Response().Header().Set("Cache-Control", "public, max-age=86400")
	return c.Blob(http.StatusOK, "image/jpeg", data)
}
