package router

import (
	"context"
	"sync"
	"time"
)

// maxParams is the maximum number of route parameters supported without
// reallocating. It comfortably covers the common case (e.g.
// /orgs/{orgID}/users/{userID}/posts/{postID}); routes with more params
// than this still work, they just reallocate the backing array.
const maxParams = 8

// contextKey is an unexported type for the router's context key,
// avoiding collisions with keys from other packages.
type contextKey struct{ name string }

// RouteCtxKey is the key used to store the routing *Context inside a
// standard context.Context.
var RouteCtxKey = &contextKey{"RouteContext"}

// routeParams tracks URL parameters using fixed-size arrays instead of
// dynamic slices, avoiding a slice allocation per request as long as the
// number of params stays within maxParams.
type routeParams struct {
	keys   [maxParams]string
	values [maxParams]string
	n      int
}

func (p *routeParams) add(key, value string) {
	if p.n < maxParams {
		p.keys[p.n] = key
		p.values[p.n] = value
		p.n++
		return
	}
	// Rare case (more than maxParams): shouldn't happen in normal use.
	// Silently ignoring would keep zero-alloc; we prefer not to support
	// this case for now and revisit if needed.
}

// get searches from the most recent entry to the oldest (not from the
// first to the last), so that in routes nested via Mount/Route that
// reuse the same key (the most common case: "*" used both by the
// internal mount mechanism and by a user catch-all), the value from the
// innermost scope (most recent) always takes precedence over a value
// from an already-resolved, more outer mount level.
func (p *routeParams) get(key string) string {
	for i := p.n - 1; i >= 0; i-- {
		if p.keys[i] == key {
			return p.values[i]
		}
	}
	return ""
}

func (p *routeParams) reset() {
	p.n = 0
}

// Context is the default routing context, attached to the in-flight
// request. It fully implements context.Context (Deadline, Done, Err,
// Value), delegating to the parent when the value isn't its own — this
// avoids the extra context.WithValue wrapper and allows reuse via
// sync.Pool.
type Context struct {
	parent context.Context

	// RoutePath is an optional override of the routing path, used
	// during the tree lookup (e.g. by Mux when routing sub-trees).
	RoutePath string

	// routeRest holds, with no allocation (it's a plain slice over the
	// original path, not a new string), the remainder of the path —
	// including the leading slash — from the point where a catch-all
	// matched. Used internally by Mount to build the sub-router's
	// RoutePath without paying the cost of a string concatenation per
	// request.
	routeRest string

	// RouteMethod is an optional override of the HTTP method.
	RouteMethod string

	// RoutePatterns is the history of patterns matched across the
	// sub-router hierarchy, for introspection (e.g. naming observability
	// spans with the pattern instead of the path with real values).
	RoutePatterns []string

	params routeParams
}

// pool reuses *Context instances across requests.
var ctxPool = sync.Pool{
	New: func() any { return new(Context) },
}

// NewRouteContext returns a new, empty Context obtained from the pool.
func NewRouteContext() *Context {
	return ctxPool.Get().(*Context)
}

// putRouteContext returns a Context to the pool after resetting it. It
// must only be called when no external reference to the Context
// survives the request (never keep a *Context after the handler
// returns).
func putRouteContext(x *Context) {
	x.Reset()
	ctxPool.Put(x)
}

// Reset returns the Context to its initial state for reuse.
func (x *Context) Reset() {
	x.parent = nil
	x.RoutePath = ""
	x.routeRest = ""
	x.RouteMethod = ""
	x.RoutePatterns = x.RoutePatterns[:0]
	x.params.reset()
}

// RouteContext returns the routing *Context stored in a request's
// context.Context, or nil if there is none.
func RouteContext(ctx context.Context) *Context {
	rc, _ := ctx.Value(RouteCtxKey).(*Context)
	return rc
}

// URLParam returns the value of the matching parameter from the
// request's routing context.
func (x *Context) URLParam(key string) string {
	return x.params.get(key)
}

// RoutePattern returns the full routing pattern matched up to the point
// of the call. It should be used after next.ServeHTTP, since the value
// changes throughout execution (see the instrumentation example below).
//
//	func Instrument(next http.Handler) http.Handler {
//		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			next.ServeHTTP(w, r)
//			pattern := RouteContext(r.Context()).RoutePattern()
//			measure(w, r, pattern)
//		})
//	}
func (x *Context) RoutePattern() string {
	if len(x.RoutePatterns) == 0 {
		return ""
	}
	// TODO(tree.go): concatenate compound patterns once the tree
	// implements it. For now it returns the last matched pattern.
	return x.RoutePatterns[len(x.RoutePatterns)-1]
}

// --- context.Context interface implementation ---
//
// By implementing the four methods manually (instead of using
// context.WithValue), we avoid the extra *valueCtx the stdlib would
// allocate per request. The only alloc left on the path of attaching
// this Context to an *http.Request is r.WithContext itself
// (new(Request)), which is the theoretical floor without unsafe — see
// the discussion in the project's history.

func (x *Context) Deadline() (time.Time, bool) {
	return x.parent.Deadline()
}

func (x *Context) Done() <-chan struct{} {
	return x.parent.Done()
}

func (x *Context) Err() error {
	return x.parent.Err()
}

func (x *Context) Value(key any) any {
	if key == RouteCtxKey {
		return x
	}
	return x.parent.Value(key)
}
