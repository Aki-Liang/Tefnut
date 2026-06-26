package server

import "github.com/labstack/echo/v4"

func (s *Server) pageTags(c echo.Context) error       { return c.NoContent(501) }
func (s *Server) apiUpdateMeta(c echo.Context) error  { return c.NoContent(501) }
func (s *Server) apiAddTag(c echo.Context) error      { return c.NoContent(501) }
func (s *Server) apiRemoveTag(c echo.Context) error   { return c.NoContent(501) }
func (s *Server) apiSetProgress(c echo.Context) error { return c.NoContent(501) }
func (s *Server) apiListTags(c echo.Context) error    { return c.NoContent(501) }
func (s *Server) apiCreateTag(c echo.Context) error   { return c.NoContent(501) }
func (s *Server) apiRenameTag(c echo.Context) error   { return c.NoContent(501) }
func (s *Server) apiDeleteTag(c echo.Context) error   { return c.NoContent(501) }
