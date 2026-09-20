// Package middleware provides a set of standard net/http middlewares
// (Logger, Recoverer, Timeout, RequestID, etc.) for use with the router.
//
// It lives in the same module as the core (not in a separate repo/module)
// because, at the project's current phase, the coupling between changes
// to the core and to the essential middlewares is high enough that
// versioning them together is the right choice. See the discussion on
// single-module vs multi-module in the project's history — migrating to
// a separate module in the future is a viable extraction, not a
// definitive decision.
package middleware
