package router

import (
	"context"
	"net/http"
)

// URLParam returns the URL parameter from an http.Request, or an empty
// string if the key doesn't exist or there is no associated routing
// Context.
func URLParam(r *http.Request, key string) string {
	if rc := RouteContext(r.Context()); rc != nil {
		return rc.URLParam(key)
	}
	return ""
}

// URLParamFromCtx returns the URL parameter directly from a request's
// context.Context, useful inside middlewares that only have access to
// ctx (not the full *http.Request).
func URLParamFromCtx(ctx context.Context, key string) string {
	if rc := RouteContext(ctx); rc != nil {
		return rc.URLParam(key)
	}
	return ""
}
