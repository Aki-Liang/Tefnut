package server

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/server/web"
	"Tefnut/internal/store"
)

var ratingChoices = []int{0, 1, 2, 3, 4, 5}

// templatesSub is the embedded FS used by render to parse individual page templates.
var templatesSub = web.FS

type crumb struct {
	ID   int64
	Name string
}

type browseData struct {
	Title      string
	ParentID   int64
	Q          string
	TagID      int64
	MinRating  int
	Ratings    []int
	Tags       []*store.TagCount
	Items      []*store.Node
	Breadcrumb []crumb
}

type readerData struct {
	ID               int64
	Name             string
	Author           string
	Rating           int
	PageCount        int
	LastPage         int
	Ratings          []int
	ReadingDirection string
	DisplayMode      string
}

// render clones the base layout template set, parses the given page template
// (so its {{define}} blocks override the layout defaults), executes the layout,
// and writes the result.
func render(c echo.Context, page string, data any) error {
	t, err := web.Templates.Clone()
	if err != nil {
		return err
	}
	if _, err := t.ParseFS(templatesSub, "templates/"+page); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		return err
	}
	return c.HTMLBlob(http.StatusOK, buf.Bytes())
}

func (s *Server) pageBrowse(c echo.Context) error {
	ctx := c.Request().Context()
	parent := int64(0)
	if c.Param("id") != "" {
		id, err := parseID(c, "id")
		if err != nil {
			return fail(c, http.StatusBadRequest, err)
		}
		parent = id
	}
	q := c.QueryParam("q")
	tagID, _ := strconv.ParseInt(c.QueryParam("tag"), 10, 64)
	minRating, _ := strconv.Atoi(c.QueryParam("minRating"))

	var items []*store.Node
	var err error
	title := "漫画库"
	if q != "" || tagID > 0 || minRating > 0 {
		items, err = s.nodes.Search(ctx, q, tagID, minRating, 0, 0)
		title = "搜索结果"
	} else {
		items, err = s.nodes.ListChildren(ctx, parent, 0, 0)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	tags, err := s.tags.List(ctx)
	if err != nil {
		log.Printf("server: list tags for browse: %v", err)
	}

	data := browseData{
		Title:      title,
		ParentID:   parent,
		Q:          q,
		TagID:      tagID,
		MinRating:  minRating,
		Ratings:    ratingChoices,
		Tags:       tags,
		Items:      items,
		Breadcrumb: s.breadcrumb(ctx, parent),
	}
	return render(c, "browse.html", data)
}

// breadcrumb walks parent → ancestor chain via s.nodes.Get until id == 0,
// returning crumbs in root-first order. A hard depth cap of 64 prevents
// infinite loops on self-parent or cyclic rows.
func (s *Server) breadcrumb(ctx context.Context, parent int64) []crumb {
	var crumbs []crumb
	id := parent
	for i := 0; id != 0 && i < 64; i++ {
		n, err := s.nodes.Get(ctx, id)
		if err != nil {
			break
		}
		crumbs = append([]crumb{{ID: n.ID, Name: n.Name}}, crumbs...)
		id = n.ParentID
	}
	return crumbs
}

func (s *Server) pageSettings(c echo.Context) error {
	ctx := c.Request().Context()
	scan, err := s.settings.GetScan(ctx)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	paths, err := s.paths.List(ctx)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return render(c, "settings.html", map[string]any{
		"LibraryPaths": paths, "ScanMode": scan.Mode,
		"ScanInterval": scan.Interval, "ScanDailyTime": scan.DailyTime,
	})
}

func (s *Server) pageReader(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	n, err := s.nodes.Get(ctx, id)
	if err != nil {
		return fail(c, http.StatusNotFound, err)
	}
	last, _ := s.progress.Get(ctx, id)
	dir := n.ReadingDirection
	if dir == "" {
		dir = "ltr"
	}
	displayMode := n.DisplayMode
	if displayMode == "" {
		displayMode = "single"
	}
	return render(c, "reader.html", readerData{
		ID:               n.ID,
		Name:             n.Name,
		Author:           n.Author,
		Rating:           n.Rating,
		PageCount:        n.PageCount,
		LastPage:         last,
		Ratings:          ratingChoices,
		ReadingDirection: dir,
		DisplayMode:      displayMode,
	})
}
