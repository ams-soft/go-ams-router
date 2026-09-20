package router

import (
	"net/http"
	"strings"
	"sync"
)

// Mux is the HTTP route multiplexer. It implements the Router interface
// and is safe for concurrent use once routes have been registered
// (route registration is not thread-safe and must happen before serving
// requests — the same model used by chi and most Go routers).
type Mux struct {
	tree        *node
	middlewares Middlewares

	// inline distinguishes a "root" Mux (created by NewRouter, owning its
	// own middleware wrap around the entire routing process) from an
	// "inline" Mux (returned by With/Group, which shares a root Mux's
	// tree and whose middlewares represents only the addition beyond
	// what the root already applies — never a copy of what the root
	// has).
	//
	// This distinction exists so that the middleware from Use() on the
	// root Mux can wrap the routing process itself (needed for
	// middlewares like StripSlashes/RedirectSlashes, which need to alter
	// the path BEFORE the tree decides which route matches) without ever
	// being executed twice for routes registered via With/Group.
	inline bool

	// handler is the root Mux with mx.middlewares already composed
	// around mx.routeHTTP, built lazily and concurrency-safely (via
	// handlerOnce) on the first request served. Not used by an inline
	// Mux (With/Group are never served directly).
	handler     http.Handler
	handlerOnce sync.Once

	notFoundHandler         http.HandlerFunc
	methodNotAllowedHandler http.HandlerFunc

	// routesRegistered locks out further Use() calls on an inline Mux
	// once the first route has already been registered through it — in
	// that case the middleware is compiled directly into the handler at
	// registration time, so a late Use() would leave already-registered
	// routes without that middleware. On the root Mux, this lock isn't
	// needed (the root's middleware is applied once, on the first
	// ServeHTTP, not per route) — there the guard is mx.handler != nil.
	routesRegistered bool

	// mounts records, for introspection via Routes(), the sub-routers
	// attached via Mount — kept separate from the tree because mount
	// nodes in the tree only hold the final http.Handler, not a
	// reference to the original Router (see node.isMountPoint and
	// Mux.Routes()).
	mounts []Route
}

// NewMux returns a freshly initialized Mux that implements Router.
func NewMux() *Mux {
	return &Mux{tree: &node{}}
}

// NewRouter is an alias for NewMux, for familiarity with chi's API.
func NewRouter() *Mux {
	return NewMux()
}

// Use adds one or more middlewares to the Mux's stack.
//
// On the root Mux, these middlewares wrap the entire routing process
// (they run BEFORE the tree decides which route matches) — this is what
// allows middlewares like StripSlashes to alter r.URL.Path before the
// match. They can be added at any point before the first request is
// served (panics after that).
//
// On an inline Mux (returned by With/Group), Use() only affects routes
// registered through this specific Mux, and must be called before any
// route registration on it (panics after that), since these
// middlewares are compiled directly into the handler at registration
// time.
func (mx *Mux) Use(mws ...func(http.Handler) http.Handler) {
	if mx.inline {
		if mx.routesRegistered {
			panic("router: middlewares added via With/Group must be registered before routes on this inline Router")
		}
	} else if mx.handler != nil {
		panic("router: all middlewares must be registered before the Mux starts serving requests")
	}
	mx.middlewares = append(mx.middlewares, mws...)
}

// With returns a new "inline" Router: it shares the same route tree, but
// appends mws to the inline middleware stack accumulated so far (empty
// if mx is the root Mux). Routes registered through the returned value
// have this inline middleware compiled into the handler at registration
// time — the root's middleware (Use on the root Mux) is not repeated
// here, because it is already applied once at the routing level.
func (mx *Mux) With(mws ...func(http.Handler) http.Handler) Router {
	var base Middlewares
	if mx.inline {
		base = mx.middlewares
	}
	combined := make(Middlewares, len(base)+len(mws))
	copy(combined, base)
	copy(combined[len(base):], mws)
	return &Mux{
		tree:                    mx.tree,
		inline:                  true,
		middlewares:             combined,
		notFoundHandler:         mx.notFoundHandler,
		methodNotAllowedHandler: mx.methodNotAllowedHandler,
	}
}

// Group creates a new inline Router on the same route path, inheriting
// the inline middleware accumulated so far — Use() calls inside fn
// extend this copy without affecting the original Mux or other sibling
// groups.
func (mx *Mux) Group(fn func(r Router)) Router {
	im := mx.With()
	if fn != nil {
		fn(im)
	}
	return im
}

// Route creates an independent sub-Mux (its own tree), runs fn on it,
// and mounts it at pattern via Mount.
func (mx *Mux) Route(pattern string, fn func(r Router)) Router {
	sub := NewRouter()
	if fn != nil {
		fn(sub)
	}
	mx.Mount(pattern, sub)
	return sub
}

// bakedHandler wraps h with this Mux's inline middleware, if any (never
// with the root's middleware, which is applied once at the routing
// level — see Use/ServeHTTP).
func (mx *Mux) bakedHandler(h http.Handler) http.Handler {
	if mx.inline {
		return mx.middlewares.Handler(h)
	}
	return h
}

// Mount attaches another http.Handler (typically another *Mux) along
// pattern. The mounted handler receives, via
// RouteContext(r.Context()).RoutePath, the path already relative to the
// mount point — allowing a sub-router to register its own routes
// starting from "/", regardless of where it was mounted on the parent
// router. If h also implements Routes (as any *Mux does), Mux.Routes()
// on the parent reflects h as Route.SubRoutes, preserving the mount
// hierarchy for introspection tooling.
func (mx *Mux) Mount(pattern string, h http.Handler) {
	mountPattern := strings.TrimSuffix(pattern, "/")

	// Exact case: request hits the mount point exactly (e.g. GET /admin
	// when mounted at "/admin"). The sub-router sees "/".
	exactHandler := mx.bakedHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		RouteContext(r.Context()).RoutePath = "/"
		h.ServeHTTP(w, r)
	}))

	// Suffix case: what's left of the path after the mount point is
	// already computed by the catch-all match itself as a slice of the
	// original string (routeRest, no allocation) — see tree.go find().
	wildcardHandler := mx.bakedHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rctx := RouteContext(r.Context())
		rctx.RoutePath = rctx.routeRest
		h.ServeHTTP(w, r)
	}))

	mx.routesRegistered = true
	var exactNode, wildcardNode *node
	for mt := methodTyp(0); mt < numMethods; mt++ {
		wildcardNode = mx.tree.insert(mt, mountPattern+"/*", wildcardHandler)
		if mountPattern != "" {
			exactNode = mx.tree.insert(mt, mountPattern, exactHandler)
		}
	}
	if wildcardNode != nil {
		wildcardNode.isMountPoint = true
	}
	if exactNode != nil {
		exactNode.isMountPoint = true
	}

	entry := Route{Pattern: pattern}
	if sr, ok := h.(Routes); ok {
		entry.SubRoutes = sr
	} else {
		entry.Handlers = map[string]http.Handler{"*": h}
	}
	mx.mounts = append(mx.mounts, entry)
}

// handle registers h for method mt at pattern, with this Mux's inline
// middleware already compiled in (composed once, here — not per
// request, and not including the root's middleware, applied
// separately).
func (mx *Mux) handle(mt methodTyp, pattern string, h http.Handler) {
	mx.routesRegistered = true
	mx.tree.insert(mt, pattern, mx.bakedHandler(h))
}

// Handle registers h for all supported HTTP methods at pattern.
func (mx *Mux) Handle(pattern string, h http.Handler) {
	for mt := methodTyp(0); mt < numMethods; mt++ {
		mx.handle(mt, pattern, h)
	}
}

// HandleFunc is equivalent to Handle, accepting http.HandlerFunc.
func (mx *Mux) HandleFunc(pattern string, h http.HandlerFunc) {
	mx.Handle(pattern, h)
}

// Method registers h for the given HTTP method at pattern. Only the
// nine standard methods are supported (see methodIndex).
func (mx *Mux) Method(method, pattern string, h http.Handler) {
	mt, ok := methodIndex(method)
	if !ok {
		panic("router: unsupported HTTP method: " + method)
	}
	mx.handle(mt, pattern, h)
}

// MethodFunc is equivalent to Method, accepting http.HandlerFunc.
func (mx *Mux) MethodFunc(method, pattern string, h http.HandlerFunc) {
	mx.Method(method, pattern, h)
}

func (mx *Mux) Connect(pattern string, h http.HandlerFunc) { mx.handle(mCONNECT, pattern, h) }
func (mx *Mux) Delete(pattern string, h http.HandlerFunc)  { mx.handle(mDELETE, pattern, h) }
func (mx *Mux) Get(pattern string, h http.HandlerFunc)     { mx.handle(mGET, pattern, h) }
func (mx *Mux) Head(pattern string, h http.HandlerFunc)    { mx.handle(mHEAD, pattern, h) }
func (mx *Mux) Options(pattern string, h http.HandlerFunc) { mx.handle(mOPTIONS, pattern, h) }
func (mx *Mux) Patch(pattern string, h http.HandlerFunc)   { mx.handle(mPATCH, pattern, h) }
func (mx *Mux) Post(pattern string, h http.HandlerFunc)    { mx.handle(mPOST, pattern, h) }
func (mx *Mux) Put(pattern string, h http.HandlerFunc)     { mx.handle(mPUT, pattern, h) }
func (mx *Mux) Trace(pattern string, h http.HandlerFunc)   { mx.handle(mTRACE, pattern, h) }

// NotFound sets the response handler for paths with no matched route.
// The default is http.NotFound.
func (mx *Mux) NotFound(h http.HandlerFunc) { mx.notFoundHandler = h }

// MethodNotAllowed sets the response handler for matched paths whose
// method isn't registered. The default responds 405 with the Allow
// header filled in.
func (mx *Mux) MethodNotAllowed(h http.HandlerFunc) { mx.methodNotAllowedHandler = h }

func (mx *Mux) notFound(w http.ResponseWriter, r *http.Request) {
	if mx.notFoundHandler != nil {
		mx.notFoundHandler(w, r)
		return
	}
	http.NotFound(w, r)
}

func (mx *Mux) methodNotAllowed(w http.ResponseWriter, r *http.Request, allowed []string) {
	if mx.methodNotAllowedHandler != nil {
		mx.methodNotAllowedHandler(w, r)
		return
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	w.WriteHeader(http.StatusMethodNotAllowed)
}

// Routes returns the routing tree as a navigable structure, for
// introspection. Sub-routers attached via Mount appear with
// Route.SubRoutes filled in (when the mounted handler also implements
// Routes), preserving the hierarchy instead of flattening everything
// into a list.
func (mx *Mux) Routes() []Route {
	routes := mx.tree.routes()
	return append(routes, mx.mounts...)
}

// Middlewares returns the list of middlewares in use on this Mux.
func (mx *Mux) Middlewares() Middlewares {
	return mx.middlewares
}

// Match looks up a handler in the tree that matches method/path,
// without executing it.
func (mx *Mux) Match(rctx *Context, method, path string) bool {
	mt, ok := methodIndex(method)
	if !ok {
		return false
	}
	n, _ := mx.tree.find(rctx, mt, path)
	return n != nil
}

// Find looks up the pattern in the tree that matches method/path.
func (mx *Mux) Find(rctx *Context, method, path string) string {
	mt, ok := methodIndex(method)
	if !ok {
		return ""
	}
	n, _ := mx.tree.find(rctx, mt, path)
	if n == nil {
		return ""
	}
	return n.pattern
}

// ServeHTTP implements http.Handler. On the root Mux, it builds (once,
// concurrency-safely via sync.Once) the wrap of mx.middlewares around
// mx.routeHTTP — this wrap is what makes Use()-level middleware run
// BEFORE the routing itself, needed for StripSlashes/RedirectSlashes and
// any middleware that needs to inspect or alter the request before
// knowing which route will match.
//
// An inline Mux (With/Group) is never served directly in normal use —
// but if it is, it falls through the same path, applying only the
// inline middleware accumulated on it (without the root's, which in
// that case never ran).
func (mx *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mx.handlerOnce.Do(func() {
		mx.handler = mx.middlewares.Handler(http.HandlerFunc(mx.routeHTTP))
	})
	mx.handler.ServeHTTP(w, r)
}

// routeHTTP does the actual work: it obtains (or creates, if this is
// the root Mux in the mount chain) the routing Context, looks up the
// matching node in the tree, and invokes the pre-composed handler
// stored on it.
//
// The Context, when freshly created, is used directly as the request's
// context.Context (r.WithContext(rctx)) — since Context implements the
// whole interface (Deadline/Done/Err/Value), we avoid the extra wrapper
// context.WithValue would allocate. The only alloc left on the happy
// path is r.WithContext itself (new(Request)), which is the theoretical
// floor without resorting to unsafe — see the decision recorded in the
// project's history.
func (mx *Mux) routeHTTP(w http.ResponseWriter, r *http.Request) {
	rctx := RouteContext(r.Context())
	isRoot := rctx == nil
	if isRoot {
		rctx = NewRouteContext()
		rctx.parent = r.Context()
		r = r.WithContext(rctx)
		defer putRouteContext(rctx)
	}

	path := rctx.RoutePath
	if path == "" {
		path = r.URL.Path
		if path == "" {
			path = "/"
		}
	}

	mt, ok := methodIndex(r.Method)
	if !ok {
		mx.notFound(w, r)
		return
	}

	n, allowed := mx.tree.find(rctx, mt, path)
	if n == nil {
		if len(allowed) > 0 {
			mx.methodNotAllowed(w, r, allowed)
			return
		}
		mx.notFound(w, r)
		return
	}

	rctx.RoutePatterns = append(rctx.RoutePatterns, n.pattern)
	n.handlers[mt].ServeHTTP(w, r)
}
