package handler

import (
	"Tefnut/internal/domain/dto"
	"Tefnut/internal/handler/handler_impl"
	"context"
	"github.com/labstack/echo/v4"
)

type TefnutHandler struct {
	impl *handler_impl.TefnutHandlerImpl
}

func NewTefnutHandler() *TefnutHandler {
	return &TefnutHandler{}
}

func (handler *TefnutHandler) SetImpl(impl *handler_impl.TefnutHandlerImpl) *TefnutHandler {
	handler.impl = impl
	return handler
}

func (handler *TefnutHandler) LibList(c echo.Context) error {
	req := &dto.LibraryListRequest{}
	err := c.Bind(req)
	if err != nil {
		handler.response(c, nil, err)
	}
	resp, err := handler.impl.LibraryList(context.Background(), req)
	return handler.response(c, resp, err)
}

func (handler *TefnutHandler) response(c echo.Context, resp interface{}, err error) error {
	if err != nil {
		return c.JSON(200, map[string]interface{}{
			"code":    -1,
			"message": err.Error(),
		})
	}
	return c.JSON(200, map[string]interface{}{
		"code":    0,
		"message": "success",
		"data":    resp,
	})
}
