// Package router é um router HTTP leve, idiomático e composável, focado em
// zero allocations no hot path e 100% de compatibilidade com net/http.
package router

import "net/http"

// Router é o conjunto de métodos de roteamento centrais, usando apenas o
// net/http padrão da stdlib. Qualquer tipo que satisfaça essa interface
// pode ser usado como router ou sub-router.
type Router interface {
	http.Handler
	Routes

	// Use adiciona um ou mais middlewares à stack do Router.
	Use(middlewares ...func(http.Handler) http.Handler)

	// With adiciona middlewares inline para um handler específico, sem
	// afetar a stack global do Router.
	With(middlewares ...func(http.Handler) http.Handler) Router

	// Group cria um novo Router inline no mesmo caminho de rota, com uma
	// stack de middleware própria (cópia da atual).
	Group(fn func(r Router)) Router

	// Route monta um sub-Router ao longo de um pattern.
	Route(pattern string, fn func(r Router)) Router

	// Mount anexa outro http.Handler (ou Router) ao longo de ./pattern/*.
	Mount(pattern string, h http.Handler)

	// Handle e HandleFunc adicionam rotas que casam com qualquer método HTTP.
	Handle(pattern string, h http.Handler)
	HandleFunc(pattern string, h http.HandlerFunc)

	// Method e MethodFunc adicionam rotas para um método HTTP específico.
	Method(method, pattern string, h http.Handler)
	MethodFunc(method, pattern string, h http.HandlerFunc)

	// Roteamento por método HTTP.
	Connect(pattern string, h http.HandlerFunc)
	Delete(pattern string, h http.HandlerFunc)
	Get(pattern string, h http.HandlerFunc)
	Head(pattern string, h http.HandlerFunc)
	Options(pattern string, h http.HandlerFunc)
	Patch(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
	Put(pattern string, h http.HandlerFunc)
	Trace(pattern string, h http.HandlerFunc)

	// NotFound define o handler de resposta quando nenhuma rota casa.
	NotFound(h http.HandlerFunc)

	// MethodNotAllowed define o handler de resposta quando o método não é
	// permitido para o pattern casado.
	MethodNotAllowed(h http.HandlerFunc)
}

// Routes expõe a árvore de roteamento para introspecção — usado por
// ferramentas de documentação e pelo próprio Router para composição.
type Routes interface {
	// Routes retorna a árvore de roteamento em uma estrutura navegável.
	Routes() []Route

	// Middlewares retorna a lista de middlewares em uso.
	Middlewares() Middlewares

	// Match busca na árvore um handler que case com method/path, sem
	// executar o handler.
	Match(rctx *Context, method, path string) bool

	// Find busca na árvore o pattern que casa com method/path.
	Find(rctx *Context, method, path string) string
}

// Route descreve os detalhes de um handler de roteamento.
type Route struct {
	SubRoutes Routes
	Handlers  map[string]http.Handler
	Pattern   string
}

// Middlewares é uma cadeia de middlewares padrão net/http, com métodos de
// composição.
type Middlewares []func(http.Handler) http.Handler

// Chain retorna um Middlewares a partir de um slice de middleware handlers.
func Chain(middlewares ...func(http.Handler) http.Handler) Middlewares {
	return Middlewares(middlewares)
}

// Handler constrói e retorna um http.Handler a partir da cadeia de
// middlewares, com h como handler final. A composição acontece uma única
// vez (no registro da rota), não por requisição.
func (mws Middlewares) Handler(h http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// HandlerFunc é equivalente a Handler, mas aceita http.HandlerFunc.
func (mws Middlewares) HandlerFunc(h http.HandlerFunc) http.Handler {
	return mws.Handler(h)
}

// WalkFunc é o tipo de função chamada para cada método e rota visitados
// por Walk.
type WalkFunc func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error

// Walk percorre qualquer árvore de router que implemente Routes.
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
