# AMS Router

A lightweight, idiomatic, composable HTTP router for building services in
Go — an alternative to [chi](https://github.com/go-chi/chi), focused on
**zero allocations on the hot path** without giving up full compatibility
with `net/http`.

- ⚡️ **Fast** — per-segment trie routing tree + method dispatch via a fixed
  array, with no `map[string]http.Handler` on the request path.
- 🔥 **Robust** — 32 tests (including `-race`), covering static routes,
  parameters, compound segments, catch-all, strict trailing slash,
  nested `Mount`/`Route`, and middleware ordering.
- 📼 **Zero external dependencies** — stdlib only.
- 🚀 **Lightweight** — small core, one responsibility per file.
- 🧊 **1 alloc/op** across the entire routing path — static, param,
  multiple params, compound segment, chained middleware, and nested
  `Mount`. This is the theoretical floor for any router that is 100%
  compatible with `http.Handler` without resorting to `unsafe` (see
  [Architecture](#architecture--design-decisions)).

## Installation

```
go get github.com/ams-soft/go-ams-router
```

Requires Go 1.27+ (see [Why Go 1.27](#why-go-127)).

## Quick usage

```go
package main

import (
	"fmt"
	"net/http"

	router "github.com/ams-soft/go-ams-router"
	"github.com/ams-soft/go-ams-router/middleware"
)

func main() {
	r := router.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintln(w, "welcome")
	})

	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "user: %s\n", router.URLParam(req, "id"))
	})

	http.ListenAndServe(":3000", r)
}
```

See `_examples/basic/main.go` for a more complete example, including
`Group`, `Route`+`Mount`, and wildcards.

## API

The `Router` interface covers the same set of operations as chi:
`Use`, `With`, `Group`, `Route`, `Mount`, `Handle`/`HandleFunc`,
`Method`/`MethodFunc`, one method per standard HTTP verb (`Get`, `Post`,
`Put`, `Patch`, `Delete`, `Head`, `Options`, `Connect`, `Trace`),
`NotFound`, and `MethodNotAllowed`. `URLParam(r, key)` and
`URLParamFromCtx(ctx, key)` read route parameters; `URLParam(r, "*")`
reads the value captured by a catch-all (`/files/*`).

The `middleware` subpackage (same module — see
[Repository structure](#repository-structure)) provides `Logger`,
`Recoverer`, `RequestID`, `Timeout`, `StripSlashes`, and
`RedirectSlashes`.

## Trailing slash is strict by default

`/user/{name}` and `/user/{name}/` are **different** routes — each one
must be registered explicitly to respond. This is intentional (it avoids
silent ambiguity about which variant the client intended) and matches
chi's default behavior.

Catch-all is the deliberate exception: `/files/*` matches both
`/files/a/b` and `/files/a/b/`, because the nature of a wildcard is to
capture "the rest of the path", with or without content after the
trailing slash.

Anyone who prefers tolerance (treating `/foo` and `/foo/` as the same
route) can use one of two middlewares, registered with `Use()` **on the
root Mux** (they need to run before routing decides which route
matches — see [Architecture](#architecture--design-decisions)):

```go
r := router.NewRouter()
r.Use(middleware.StripSlashes)   // silently normalizes
// or:
r.Use(middleware.RedirectSlashes) // responds 301 removing the trailing slash
```

## Compound segments (multiple params in one segment)

Beyond the common case — `{id}` occupying the entire segment — patterns
with `{param}` mixed with literal text in the same segment are also
supported:

```go
r.Get("/articles/{month}-{day}-{year}", handler)
// GET /articles/01-16-2017 → month=01, day=16, year=2017
```

The pattern is decomposed into literal+param once, at route registration
time; at runtime, each param is matched via `strings.Index` against the
next literal (no `regexp`), so a compound segment is just as zero-alloc
as `{id}` alone (see benchmarks below).

## Architecture & design decisions

### The theoretical allocation floor with `http.Handler`

Any router that keeps the standard `func(w http.ResponseWriter, r
*http.Request)` signature — instead of a custom signature like `func(w,
r, params)` — needs to attach route parameters via `r.WithContext(ctx)`,
because there is no other standard place to put them.
`Request.WithContext` always allocates a new `*Request`
(`r2 := new(Request)`). That's the floor: **1 alloc/op**, unavoidable
without resorting to `unsafe` to write directly to the unexported
`Request.ctx` field (deliberately ruled out in this project — the gain
doesn't offset the risk of breaking with every Go version change).

The benchmarks below confirm the router consistently hits this floor,
even on routes with multiple parameters, chained middleware, nested
`Mount`, and under `-race`:

```
$ go test -race ./...
ok  	github.com/ams-soft/go-ams-router	1.011s
ok  	github.com/ams-soft/go-ams-router/middleware	1.017s

$ go test -run=^$ -bench=. -benchmem ./...
BenchmarkStaticRoute        230.1 ns/op   320 B/op   1 allocs/op
BenchmarkParamRoute         191.2 ns/op   320 B/op   1 allocs/op
BenchmarkFiveParams         358.6 ns/op   320 B/op   1 allocs/op
BenchmarkMiddlewareChain    255.9 ns/op   320 B/op   1 allocs/op
BenchmarkMount              356.9 ns/op   320 B/op   1 allocs/op
BenchmarkCompoundSegment    323.6 ns/op   320 B/op   1 allocs/op
```

(reference machine: Intel i9-12900HX, Go 1.27.1, linux/amd64 — absolute
numbers vary by hardware; what matters is the constant **1 allocs/op**
across every path, including the compound segment
(`{month}-{day}-{year}`), matched by delimiter scanning instead of
regexp — see `matchCompound` in `tree.go`. Reproduce with the commands
above.)

How this is achieved:

1. **`Context` implements `context.Context` manually** (`Deadline`,
   `Done`, `Err`, `Value`) instead of using `context.WithValue` — which
   would allocate an additional `*valueCtx` on top of `WithContext`.
   `Context` is recycled via `sync.Pool` between requests.
2. **Parameters in a fixed array** (`[8]string` for keys and values),
   not a dynamic slice — no slice allocation per request as long as the
   number of params stays within the limit (`maxParams`, currently 8).
3. **Method dispatch via array** (`[9]http.Handler` per node), resolved
   by a `switch` on the method string — no map hashing.
4. **Middleware compiled at route registration, not per request.**
   There are two layers: the root Mux middleware (via `Use()`) wraps the
   entire routing process — composed once via `sync.Once`, on the first
   request, and runs BEFORE the tree match (this is what allows
   `StripSlashes` to alter the path before routing decides the route);
   inline middleware (via `With()`/`Group()`) is compiled directly into
   each route's handler at registration time. No closure composition
   happens per request in either case, and neither is executed twice
   over (the inline layer never includes a copy of what the root layer
   already covers).
5. **Mount without string concatenation**: the remainder of the path
   after the mount point is obtained via a *slice* of the original
   string (`path[start-1:]`), not via concatenation (`"/" + value`) —
   string slicing in Go doesn't allocate because it shares the
   underlying byte array; concatenation does.

### Routing tree: per-segment trie, not a byte radix tree

The tree is organized by **path segment** (split on `/`), with four node
types — static, named parameter (`{id}`), compound (`{a}-{b}`,
decomposed into literal+param, no regexp), and catch-all (`*`) — instead
of a byte-level radix tree like chi/httprouter's (which compresses
prefixes byte by byte). This deliberate choice trades a slice of
peak performance for much less bug surface: fan-out per level tends to
be small in practice (few sibling resources under the same prefix), so
the linear scan over `children` is fast and allocation-free.

**Known limitation, documented for anyone extending the project:** only
the 9 standard HTTP methods are supported (see `method.go`) — there is
no mechanism for registering custom methods (e.g. `PURGE`, `LOCK`).
Anyone who needs this can add a "method catch-all" node type or a
`map[string]http.Handler` fallback per node, isolated from the fixed
array of the 9 standard methods so as not to impose extra cost on those
who don't use it.

### Why not FFI/Rust

This was considered and dropped: the overhead of a `cgo` call
(100-200ns, stack switch, inability to use normal goroutine preemption)
is greater than the total path-matching time it would aim to speed up,
and it would break zero-dependencies, trivial cross-compilation
(`GOOS`/`GOARCH` with no extra toolchain), and Go's concurrency model
under high load.

## Repository structure

Single module: one `go.mod` at the root, covering the core and the
`middleware` subpackage. This choice was deliberate for the project's
current phase — the coupling between changes to the core and to the
essential middlewares during development is high enough that versioning
them together is simpler and safer than a multi-module setup (multiple
`go.mod` files via `go.work`). Extracting `middleware/` (or other future
satellite packages, such as `render`/`docgen`, in the spirit of what chi
does with separate repos) into its own module is a viable migration down
the road, not a definitive decision.

```
go-router/
├── go.mod
├── LICENSE
├── router.go                        // Router, Routes, Route, Middlewares, Walk interfaces
├── context.go                       // Context (implements context.Context), pool, routeParams
├── params.go                        // URLParam / URLParamFromCtx (package-level)
├── method.go                        // method dispatch via array (9 standard methods)
├── tree.go                          // routing tree (per-segment trie + compound without regex)
├── mux.go                           // Mux: concrete Router implementation + ServeHTTP
├── router_test.go                   // correctness tests
├── router_bench_test.go             // benchmarks (allocs/op)
├── race_test.go                     // concurrency test on the first request (-race)
├── strip_slashes_integration_test.go // StripSlashes/RedirectSlashes + strict routing
├── middleware/
│   ├── doc.go
│   ├── logger.go
│   ├── recoverer.go
│   ├── request_id.go
│   ├── response_writer.go
│   ├── strip_slashes.go
│   ├── timeout.go
│   └── middleware_test.go
└── _examples/
    └── basic/main.go                // "_" prefix — ignored by go build/test ./...
```

## Why Go 1.27

- **"Size-specialized" memory allocation**: the compiler generates calls
  to size-specialized allocation routines, reducing the cost of small
  allocations (<80 bytes) by up to 30% — exactly the profile of the
  `*Context` and `*http.Request` this router allocates per request.
- **Generic methods**: enable, in the future, an optional typed layer on
  top of the core (e.g. automatic param binding to a struct) without
  polluting the core's `http.Handler` signature.
- **`uuid` package in stdlib**: makes it possible to add an `{id:uuid}`
  type matcher in the future without breaking the zero external
  dependencies requirement.

## Running the tests

```
go test ./...                       # correctness
go test -race ./...                 # concurrency
go test -run=^$ -bench=. -benchmem ./...  # performance (allocs/op)
go vet ./...
```

## Roadmap / possible extensions

- [ ] Type matcher (`{id:int}`, `{id:uuid}`) using Go 1.27's stdlib
      `uuid` package
- [ ] Optional generics layer on top of the core (generic methods,
      Go 1.27+) for typed param binding
- [ ] Support for custom HTTP methods (`PURGE`, `LOCK`, etc.), via a
      lazy map per node, isolated from the fixed array of the 9 standard
      methods

## License

MIT — see [LICENSE](LICENSE).
