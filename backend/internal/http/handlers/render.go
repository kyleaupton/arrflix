package handlers

import (
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/labstack/echo/v4"
)

// RenderError emits an RFC 9457 application/problem+json response derived from
// the supplied error's typed Kind. Service code constructs typed errors via
// apperrors.* and handlers return RenderError(c, err) instead of hand-rolling
// status/body picking.
//
// Untyped errors (or KindInternal / KindUnspecified) become opaque 500s — the
// original message is intentionally hidden from the wire to avoid leaking
// internal detail. Logs still see the full error.
func RenderError(c echo.Context, err error) error {
	pd := apperrors.ToProblem(err)
	c.Response().Header().Set("Content-Type", apperrors.ContentType)
	return c.JSON(pd.Status, pd)
}
