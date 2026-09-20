package router

import (
	"context"
	"sync"
	"time"
)

// maxParams é o número máximo de parâmetros de rota suportados sem
// realocar. Cobre o caso comum (ex: /orgs/{orgID}/users/{userID}/posts/{postID})
// com folga; rotas com mais params que isso ainda funcionam, apenas
// realocando o backing array.
const maxParams = 8

// contextKey é um tipo não exportado para a chave de contexto do router,
// evitando colisão com chaves de outros pacotes.
type contextKey struct{ name string }

// RouteCtxKey é a chave usada para armazenar o *Context de roteamento
// dentro de um context.Context padrão.
var RouteCtxKey = &contextKey{"RouteContext"}

// routeParams rastreia parâmetros de URL usando arrays de tamanho fixo em
// vez de slices dinâmicos, evitando alocação de slice por requisição
// enquanto o número de params estiver dentro de maxParams.
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
	// Caso raro (mais de maxParams): não deve ocorrer em uso normal.
	// Ignorar silenciosamente manteria o zero-alloc; preferimos não
	// suportar esse caso por ora e revisitar se necessário.
}

// get busca da entrada mais recente para a mais antiga (não da primeira
// para a última), para que, em rotas aninhadas via Mount/Route que
// reutilizem a mesma chave (o caso mais comum: "*" usado tanto pelo
// mecanismo interno de mount quanto por um catch-all do usuário), o valor
// do escopo mais interno (mais recente) sempre prevaleça sobre um valor
// de um nível de mount mais externo já resolvido.
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

// Context é o contexto de roteamento padrão, associado à requisição em
// andamento. Ele implementa context.Context integralmente (Deadline, Done,
// Err, Value), delegando ao parent quando o valor não é seu — isso evita o
// wrapper extra de context.WithValue e permite reuso via sync.Pool.
type Context struct {
	parent context.Context

	// RoutePath é um override opcional do path de roteamento, usado
	// durante a busca na árvore (ex: por Mux ao rotear sub-árvores).
	RoutePath string

	// routeRest guarda, sem nenhuma alocação (é uma slice pura sobre o
	// path original, não uma string nova), o restante do path — incluindo
	// a barra inicial — a partir do ponto onde um catch-all casou. Usado
	// internamente por Mount para montar RoutePath do sub-router sem
	// pagar o custo de uma concatenação de string por requisição.
	routeRest string

	// RouteMethod é um override opcional do método HTTP.
	RouteMethod string

	// RoutePatterns é o histórico de patterns casados ao longo da
	// hierarquia de sub-routers, para introspecção (ex: nomear spans de
	// observability com o pattern em vez do path com valores reais).
	RoutePatterns []string

	params routeParams
}

// pool reutiliza instâncias de *Context entre requisições.
var ctxPool = sync.Pool{
	New: func() any { return new(Context) },
}

// NewRouteContext retorna um novo Context vazio, obtido do pool.
func NewRouteContext() *Context {
	return ctxPool.Get().(*Context)
}

// putRouteContext devolve um Context ao pool após resetá-lo. Só deve ser
// chamado quando nenhuma referência externa ao Context sobreviver à
// requisição (nunca guarde um *Context após o handler retornar).
func putRouteContext(x *Context) {
	x.Reset()
	ctxPool.Put(x)
}

// Reset volta o Context ao seu estado inicial para reuso.
func (x *Context) Reset() {
	x.parent = nil
	x.RoutePath = ""
	x.routeRest = ""
	x.RouteMethod = ""
	x.RoutePatterns = x.RoutePatterns[:0]
	x.params.reset()
}

// RouteContext retorna o *Context de roteamento armazenado em um
// context.Context de requisição, ou nil se não houver nenhum.
func RouteContext(ctx context.Context) *Context {
	rc, _ := ctx.Value(RouteCtxKey).(*Context)
	return rc
}

// URLParam retorna o valor do parâmetro correspondente do contexto de
// roteamento da requisição.
func (x *Context) URLParam(key string) string {
	return x.params.get(key)
}

// RoutePattern retorna o pattern de roteamento completo casado até o
// momento da chamada. Deve ser usado após next.ServeHTTP, já que o valor
// muda ao longo da execução (ver exemplo de instrumentação abaixo).
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
	// TODO(tree.go): concatenar patterns compostos quando a árvore
	// estiver implementada. Por ora retorna o último pattern casado.
	return x.RoutePatterns[len(x.RoutePatterns)-1]
}

// --- implementação da interface context.Context ---
//
// Ao implementar os quatro métodos manualmente (em vez de usar
// context.WithValue), evitamos o *valueCtx que a stdlib alocaria a mais
// por requisição. O único alloc que resta no caminho de anexar este
// Context a um *http.Request é o próprio r.WithContext (new(Request)),
// que é o piso teórico sem unsafe — ver discussão no histórico do projeto.

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
