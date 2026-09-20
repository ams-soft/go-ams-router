// Package router is a lightweight, idiomatic, composable HTTP router,
// focused on zero allocations on the hot path and 100% compatibility
// with net/http.
package router

import "net/http"

// Router is the set of core routing methods, using only the standard
// net/http from the stdlib. Any type that satisfies this interface can
// be used as a router or sub-router.
type Router interface {
	http.Handler
	Routes

	// Use adds one or more middlewares to the Router's stack.
	Use(middlewares ...func(http.Handler) http.Handler)

	// With adds inline middlewares for a specific handler, without
	// affecting the Router's global stack.
	With(middlewares ...func(http.Handler) http.Handler) Router

	// Group creates a new inline Router on the same route path, with its
	// own middleware stack (a copy of the current one).
	Group(fn func(r Router)) Router

	// Route mounts a sub-Router along a pattern.
	Route(pattern string, fn func(r Router)) Router

	// Mount attaches another http.Handler (or Router) along ./pattern/*.
	Mount(pattern string, h http.Handler)

	// Handle and HandleFunc add routes that match any HTTP method.
	Handle(pattern string, h http.Handler)
	HandleFunc(pattern string, h http.HandlerFunc)

	// Method and MethodFunc add routes for a specific HTTP method.
	Method(method, pattern string, h http.Handler)
	MethodFunc(method, pattern string, h http.HandlerFunc)

	// Routing by HTTP method.
	Connect(pattern string, h http.HandlerFunc)
	Delete(pattern string, h http.HandlerFunc)
	Get(pattern string, h http.HandlerFunc)
	Head(pattern string, h http.HandlerFunc)
	Options(pattern string, h http.HandlerFunc)
	Patch(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
	Put(pattern string, h http.HandlerFunc)
	Trace(pattern string, h http.HandlerFunc)

	// NotFound sets the response handler for when no route matches.
	NotFound(h http.HandlerFunc)

	// MethodNotAllowed sets the response handler for when the method is
	// not allowed for the matched pattern.
	MethodNotAllowed(h http.HandlerFunc)
}

// Routes exposes the routing tree for introspection — used by
// documentation tooling and by the Router itself for composition.
type Routes interface {
	// Routes returns the routing tree as a navigable structure.
	Routes() []Route

	// Middlewares returns the list of middlewares in use.
	Middlewares() Middlewares

	// Match looks up a handler in the tree that matches method/path,
	// without executing the handler.
	Match(rctx *Context, method, path string) bool

	// Find looks up the pattern in the tree that matches method/path.
	Find(rctx *Context, method, path string) string
}

// Route describes the details of a routing handler.
type Route struct {
	SubRoutes Routes
	Handlers  map[string]http.Handler
	Pattern   string
}

// Middlewares is a chain of standard net/http middlewares, with
// composition methods.
type Middlewares []func(http.Handler) http.Handler

// Chain returns a Middlewares from a slice of middleware handlers.
func Chain(middlewares ...func(http.Handler) http.Handler) Middlewares {
	return Middlewares(middlewares)
}

// Handler builds and returns an http.Handler from the middleware chain,
// with h as the final handler. Composition happens once (at route
// registration), not per request.
func (mws Middlewares) Handler(h http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// HandlerFunc is equivalent to Handler, but accepts http.HandlerFunc.
func (mws Middlewares) HandlerFunc(h http.HandlerFunc) http.Handler {
	return mws.Handler(h)
}

// WalkFunc is the function type called for each method and route
// visited by Walk.
type WalkFunc func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error

// Walk traverses any router tree that implements Routes.
func Walk(r Routes, walkFn WalkFunc) error {
	return walk(r, walkFn, "")
}

func walk(r Routes, walkFn WalkFunc, parentRoute string, parentMw ...func(http.Handler) http.Handler) error {
	for _, route := range r.Routes() {
		mws := make([]func(http.Handler) http.Handler, len(parentMw))
		copy(mws, parentMw)
		mws = append(mws, r.Middlewares()...)

		if route.SubRoutes != nil {
			if err := walk(route.SubRoutes, walkFn, parentRoute+route.Pattern, mws...); err != nil {
				return err
			}
			continue
		}

		for method, handler := range route.Handlers {
			if err := walkFn(method, parentRoute+route.Pattern, handler, mws...); err != nil {
				return err
			}
		}
	}
	return nil
}
