package auth

import (
	"log/slog"
	"strings"

	"github.com/go-sum/foundry/pkg/web"
)

type claimsKey struct{}

func Middleware(verifier TokenVerifier, logger *slog.Logger) web.Middleware {
	return func(next web.Handler) web.Handler {
		return func(c *web.Context) (web.Response, error) {
			token := extractBearerFromHeader(c.Request.Headers.Get("Authorization"))
			if token == "" {
				logger.Warn("auth failed: missing token",
					"remote_addr", c.Request.RemoteAddr(),
					"path", c.Request.URL.Path,
				)
				return web.JSON(401, map[string]string{
					"error": "missing or invalid authorization header",
				}), nil
			}

			claims, err := verifier.Verify(c.Context(), token)
			if err != nil {
				logger.Warn("auth failed: token verification",
					"remote_addr", c.Request.RemoteAddr(),
					"path", c.Request.URL.Path,
					"error", err.Error(),
				)
				return web.JSON(401, map[string]string{
					"error": "token verification failed",
				}), nil
			}

			c.Set(claimsKey{}, claims)
			return next(c)
		}
	}
}

func ClaimsFromContext(c *web.Context) *GitHubClaims {
	claims, _ := web.Get[*GitHubClaims](c, claimsKey{})
	return claims
}

func SetClaims(c *web.Context, claims *GitHubClaims) {
	c.Set(claimsKey{}, claims)
}

func extractBearerFromHeader(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return parts[1]
}
