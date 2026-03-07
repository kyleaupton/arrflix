package middlewares

import (
	"net/http"
	"strings"

	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/labstack/echo/v4"
)

// SetupMode middleware enforces setup mode globally:
// When the system is NOT initialized, only setup routes, health, and bootstrap are allowed.
// Setup route blocking after initialization is handled by the SetupOnly group middleware.
func SetupMode(services *service.Services) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path
			ctx := c.Request().Context()

			// Health check and bootstrap are always allowed
			if path == "/health" || path == "/api/v1/bootstrap" {
				return next(c)
			}

			initialized, err := services.Setup.IsInitialized(ctx)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "setup check failed"})
			}

			if !initialized {
				// SETUP MODE: Only allow setup routes
				isSetupRoute := strings.HasPrefix(path, "/api/v1/setup/")
				if !isSetupRoute {
					return c.JSON(http.StatusServiceUnavailable, map[string]string{
						"error":     "setup required",
						"setup_url": "/api/v1/setup/status",
					})
				}
			}

			return next(c)
		}
	}
}

// SetupOnly middleware blocks requests to setup routes once the system is initialized.
// Apply this to the setup route group.
func SetupOnly(services *service.Services) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			initialized, err := services.Setup.IsInitialized(c.Request().Context())
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "setup check failed"})
			}
			if initialized {
				return c.JSON(http.StatusConflict, map[string]string{"error": "system already initialized"})
			}
			return next(c)
		}
	}
}
