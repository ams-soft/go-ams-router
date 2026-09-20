package router

import (
	"net/http"
	"strings"
)

// nodeType identifies the role of a node within the routing tree.
type nodeType uint8

const (
	ntStatic   nodeType = iota // literal segment: /users
	ntParam                    // whole named segment: /{id}
	ntCompound                 // segment with partial params: /{month}-{day}
	ntCatchAll                 // wildcard: /* — only valid as the last segment
)

// node is a node of the routing tree. The tree is organized by path
// segment (split on '/'), with four node types — it is not a byte-level
// radix tree like chi/httprouter's (which compresses prefixes byte by
// byte). This deliberate choice trades a slice of peak performance for
// much less bug surface: fan-out per level tends to be small in
// practice, so the linear scan over children is fast and allocation
// free. The common path — a single {param} occupying the whole segment —
// remains zero-alloc; compound segments (literal mixed with {param},
// e.g. "{month}-{day}-{year}") are also zero-alloc, matched by delimiter
// scanning (strings.Index) instead of regexp — see matchCompound.
type node struct {
	typ nodeType
	seg string // static: literal text; param/catchAll: parameter name

	children []*node // static children
	compound []*node // children with a compound segment (delimiter), tested in registration order
	param    *node   // at most one param child (whole segment) per node
	catchAll *node   // at most one catch-all child per node, always a leaf

	// compoundParts is only used when typ == ntCompound.
	compoundParts []compoundPart

	handlers [numMethods]http.Handler

	pattern      string // full registered pattern, used by RoutePattern()/Routes()
	isMountPoint bool   // true for nodes created by Mux.Mount() — see Mux.Routes()
}

// insert adds a handler for method mt at the given pattern, creating
// nodes as needed, and returns the final node (used by Mount to mark
// isMountPoint). Called only during route registration (startup), never
// on a request's path — so there is no zero-alloc concern here.
func (n *node) insert(mt methodTyp, pattern string, h http.Handler) *node {
	if pattern == "" || pattern[0] != '/' {
		panic("router: routing pattern must begin with '/': " + pattern)
	}

	segs := splitPattern(pattern)
	cur := n
	for _, seg := range segs {
		cur = cur.child(seg, pattern)
	}

	if cur.handlers[mt] != nil {
		panic("router: handler already registered for " + methodName(mt) + " " + pattern)
	}
	cur.handlers[mt] = h
	cur.pattern = pattern
	return cur
}

// child finds or creates cur's child corresponding to seg, deciding the
// node type from the segment's shape:
//   - "*"                             → catch-all
//   - "{name}" (whole segment)        → simple param
//   - contains "{" but isn't just that → compound, decomposed into literal+param
//   - anything else                   → static (exact literal)
func (cur *node) child(seg, fullPattern string) *node {
	switch {
	case seg == "*":
		if cur.catchAll == nil {
			cur.catchAll = &node{typ: ntCatchAll, seg: "*"}
		}
		return cur.catchAll

	case len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}' && strings.Count(seg, "{") == 1:
		name := seg[1 : len(seg)-1]
		if name == "" {
			panic("router: empty param name in pattern: " + fullPattern)
		}
		if cur.param == nil {
			cur.param = &node{typ: ntParam, seg: name}
		} else if cur.param.seg != name {
			panic("router: conflicting param names at the same position (" +
				cur.param.seg + " vs " + name + ") in pattern: " + fullPattern)
		}
		return cur.param

	case strings.Contains(seg, "{"):
		parts := parseCompound(seg, fullPattern)
		for _, c := range cur.compound {
			if c.seg == seg {
				return c
			}
		}
		child := &node{typ: ntCompound, seg: seg, compoundParts: parts}
		cur.compound = append(cur.compound, child)
		return child

	default:
		for _, c := range cur.children {
			if c.seg == seg {
				return c
			}
		}
		child := &node{typ: ntStatic, seg: seg}
		cur.children = append(cur.children, child)
		return child
	}
}

// compoundPart is a piece of a compound segment — literal text or a
// named parameter — in the order it appears in the pattern.
type compoundPart struct {
	isParam bool
	text    string // literal: exact text to match; param: parameter name
}

// parseCompound decomposes a segment like "{month}-{day}-{year}" into
// alternating literals and parameters, used only for segments with
// {param} mixed with literal text — never for the common case of a
// standalone {param} (that one stays on the ntParam path, without going
// through here).
func parseCompound(seg, fullPattern string) []compoundPart {
	var parts []compoundPart

	i := 0
	for i < len(seg) {
		if seg[i] == '{' {
			end := strings.IndexByte(seg[i:], '}')
			if end == -1 {
				panic("router: unclosed '{' in pattern segment: " + seg + " (pattern " + fullPattern + ")")
			}
			name := seg[i+1 : i+end]
			if name == "" {
				panic("router: empty param name in pattern: " + fullPattern)
			}
			parts = append(parts, compoundPart{isParam: true, text: name})
			i += end + 1
			continue
		}
		next := strings.IndexByte(seg[i:], '{')
		var literal string
		if next == -1 {
			literal = seg[i:]
			i = len(seg)
		} else {
			literal = seg[i : i+next]
			i += next
		}
		parts = append(parts, compoundPart{isParam: false, text: literal})
	}
	return parts
}

// matchCompound matches seg against parts at runtime, without regexp:
// each parameter consumes the smallest possible prefix up to the next
// literal (or to the end of the segment, if it's the last part) — the
// same criterion as a non-greedy group (.+?), computed with
// strings.Index instead of a regex engine, so it stays zero-alloc. It
// does not backtrack between parameters: if the next literal appears
// more than once in seg, it uses the first occurrence, which is the
// common case (fixed delimiters like "-" or "."). It only writes to
// rctx.params when the whole segment matches — no side effects on
// failure.
func matchCompound(parts []compoundPart, seg string, rctx *Context) bool {
	var names, values [maxParams]string
	n := 0

	pos := 0
	for pi, part := range parts {
		if !part.isParam {
			if !strings.HasPrefix(seg[pos:], part.text) {
				return false
			}
			pos += len(part.text)
			continue
		}

		var end int
		switch {
		case pi+1 >= len(parts):
			// last part: consumes the rest of the segment.
			if pos >= len(seg) {
				return false
			}
			end = len(seg)

		case !parts[pi+1].isParam:
			// delimited by the next literal.
			idx := strings.Index(seg[pos:], parts[pi+1].text)
			if idx <= 0 { // -1: no delimiter; 0: empty param (.+ requires >=1 char)
				return false
			}
			end = pos + idx

		default:
			// param followed by another param with no literal in between:
			// degenerate, undocumented case — each one consumes 1 char
			// (the minimum an unconstrained (.+?) would capture).
			if pos >= len(seg) {
				return false
			}
			end = pos + 1
		}

		if n >= maxParams {
			return false
		}
		names[n], values[n] = part.text, seg[pos:end]
		n++
		pos = end
	}

	if pos != len(seg) {
		return false
	}

	for i := 0; i < n; i++ {
		rctx.params.add(names[i], values[i])
	}
	return true
}

// find walks the tree looking for the node whose pattern matches
// method/path. Parameters found along the way — including those from
// compound segments, via matchCompound — are written directly into
// rctx.params (a fixed-size array, no dynamic slice), so the entire
// match is zero-alloc, not just the static/param/catch-all path.
//
// Trailing slash is significant: "/foo" and "/foo/" only match the same
// handler if both patterns were registered explicitly (or if
// StripSlashes/RedirectSlashes normalizes the path before routing — see
// the middleware package). Catch-all is the exception: it matches both
// with and without a trailing slash, since its nature is to capture "the
// rest of the path", with or without content.
//
// Return values:
//   - (node, nil)      when there is a handler for mt on the matched pattern
//   - (nil, allowed)   when the pattern matches but not for method mt
//     (allowed lists the methods that do match — the only point in this
//     function that allocates outside the compound-segment case, and
//     only on the 405 error path, not the happy path)
//   - (nil, nil)       when no pattern matches (404)
func (n *node) find(rctx *Context, mt methodTyp, path string) (*node, []string) {
	if path == "/" {
		return n.finish(mt)
	}

	cur := n
	i := 1 // skip the leading slash
	ln := len(path)

	for i <= ln {
		start := i
		for i < ln && path[i] != '/' {
			i++
		}
		seg := path[start:i]
		trailingEmpty := seg == "" && i == ln

		if seg == "" && !trailingEmpty {
			// double slash in the middle of the path: tolerate and continue.
			i++
			continue
		}

		matched := false

		for _, c := range cur.children {
			if c.seg == seg {
				cur = c
				matched = true
				break
			}
		}

		if !matched && !trailingEmpty {
			for _, c := range cur.compound {
				if matchCompound(c.compoundParts, seg, rctx) {
					cur = c
					matched = true
					break
				}
			}
		}

		if !matched && !trailingEmpty && cur.param != nil {
			rctx.params.add(cur.param.seg, seg)
			cur = cur.param
			matched = true
		}

		if !matched && cur.catchAll != nil {
			rctx.params.add("*", path[start:])
			// path[start-1:] includes the slash preceding the segment —
			// a slice of the same string (no allocation), used
			// internally by Mount. When trailingEmpty, start==ln and
			// path[start-1:] is just "/", which is the correct
			// RoutePath for the sub-router.
			rctx.routeRest = path[start-1:]
			cur = cur.catchAll
			matched = true
			i = ln
		}

		if !matched {
			return nil, nil
		}
		i++
	}

	return cur.finish(mt)
}

// finish resolves n's final handler for method mt, or builds the list
// of allowed methods for a 405 response.
func (n *node) finish(mt methodTyp) (*node, []string) {
	if n.handlers[mt] != nil {
		return n, nil
	}

	var allowed []string
	for m := methodTyp(0); m < numMethods; m++ {
		if n.handlers[m] != nil {
			allowed = append(allowed, methodName(m))
		}
	}
	if len(allowed) > 0 {
		return nil, allowed
	}
	return nil, nil
}

// splitPattern splits a pattern into segments. An explicit trailing
// slash (except for the root pattern "/") produces an extra empty
// segment at the end, preserving the distinction between "/foo" and
// "/foo/" — see find() for how this is matched against the actual
// request path.
func splitPattern(pattern string) []string {
	body := strings.TrimPrefix(pattern, "/")
	if body == "" {
		return nil // root pattern "/": zero segments
	}

	hasTrailingSlash := strings.HasSuffix(body, "/")
	core := body
	if hasTrailingSlash {
		core = strings.TrimSuffix(body, "/")
	}

	var segs []string
	if core != "" {
		for _, p := range strings.Split(core, "/") {
			if p != "" {
				segs = append(segs, p)
			}
		}
	}
	if hasTrailingSlash {
		segs = append(segs, "")
	}
	return segs
}

// routes recursively collects all routes registered from this node
// onward, for introspection (Mux.Routes()). Nodes marked as a mount
// point are skipped here — Mux.Routes() represents them separately, as
// Route.SubRoutes, from Mux.mounts. Not used on the request hot path, so
// allocating here is acceptable.
func (n *node) routes() []Route {
	if n.isMountPoint {
		return nil
	}

	var out []Route

	handlers := make(map[string]http.Handler)
	for m := methodTyp(0); m < numMethods; m++ {
		if n.handlers[m] != nil {
			handlers[methodName(m)] = n.handlers[m]
		}
	}
	if len(handlers) > 0 {
		out = append(out, Route{Pattern: n.pattern, Handlers: handlers})
	}

	for _, c := range n.children {
		out = append(out, c.routes()...)
	}
	for _, c := range n.compound {
		out = append(out, c.routes()...)
	}
	if n.param != nil {
		out = append(out, n.param.routes()...)
	}
	if n.catchAll != nil {
		out = append(out, n.catchAll.routes()...)
	}
	return out
}
