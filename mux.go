package router

import (
	"net/http"
	"strings"
	"sync"
)

// Mux é o multiplexador de rotas HTTP. Implementa a interface Router e é
// seguro para uso concorrente após o registro das rotas (registro de
// rotas não é thread-safe e deve acontecer antes de servir requisições —
// o mesmo modelo usado pelo chi e pela maioria dos routers Go).
type Mux struct {
	tree        *node
	middlewares Middlewares

	// inline distingue um Mux "raiz" (criado por NewRouter, dono de um
	// wrap de middleware próprio ao redor de todo o roteamento) de um
	// Mux "inline" (retornado por With/Group, que compartilha a árvore
	// de um Mux raiz e cujo middlewares representa só o acréscimo além
	// do que a raiz já aplica — nunca uma cópia do que a raiz tem).
	//
	// Essa distinção existe para que o middleware de Use() no Mux raiz
	// possa envolver o roteamento em si (necessário para middlewares
	// como StripSlashes/RedirectSlashes, que precisam alterar o path
	// ANTES da árvore decidir qual rota casa) sem nunca ser executado
	// duas vezes para rotas registradas via With/Group.
	inline bool

	// handler é o Mux raiz com mx.middlewares já composto ao redor de
	// mx.routeHTTP, construído de forma preguiçosa e segura para
	// concorrência (via handlerOnce) na primeira requisição servida. Não
	// é usado por Mux inline (With/Group nunca são servidos diretamente).
	handler     http.Handler
	handlerOnce sync.Once

	notFoundHandler         http.HandlerFunc
	methodNotAllowedHandler http.HandlerFunc

	// routesRegistered trava novas chamadas a Use() num Mux inline
	// depois que a primeira rota já foi registrada através dele — nesse
	// caso o middleware é compilado diretamente no handler no momento do
	// registro, então um Use() tardio deixaria rotas já registradas sem
	// esse middleware. No Mux raiz, essa trava não é necessária (o
	// middleware da raiz é aplicado uma vez, no primeiro ServeHTTP, não
	// por rota) — lá o guard é mx.handler != nil.
	routesRegistered bool

	// mounts registra, para introspecção via Routes(), os sub-routers
	// anexados via Mount — mantido separado da árvore porque os nodes de
	// mount na árvore só guardam o http.Handler final, não a referência
	// ao Router original (ver node.isMountPoint e Mux.Routes()).
	mounts []Route
}

// NewMux retorna um Mux recém-inicializado que implementa Router.
func NewMux() *Mux {
	return &Mux{tree: &node{}}
}

// NewRouter é um alias de NewMux, para familiaridade com a API do chi.
func NewRouter() *Mux {
	return NewMux()
}

// Use adiciona um ou mais middlewares à stack do Mux.
//
// No Mux raiz, esses middlewares envolvem o roteamento inteiro (rodam
// ANTES da árvore decidir qual rota casa) — é o que permite a
// middlewares como StripSlashes alterar r.URL.Path antes do match.
// Podem ser adicionados a qualquer momento antes da primeira requisição
// servida (panics depois disso).
//
// Num Mux inline (retornado por With/Group), Use() só afeta rotas
// registradas através deste Mux especificamente, e deve ser chamado
// antes de qualquer registro de rota nele (panics depois disso), já que
// esses middlewares são compilados diretamente no handler no momento do
// registro.
func (mx *Mux) Use(mws ...func(http.Handler) http.Handler) {
	if mx.inline {
		if mx.routesRegistered {
			panic("router: middlewares adicionados via With/Group devem ser registrados antes das rotas nesse Router inline")
		}
	} else if mx.handler != nil {
		panic("router: todos os middlewares devem ser registrados antes do Mux começar a servir requisições")
	}
	mx.middlewares = append(mx.middlewares, mws...)
}

// With retorna um novo Router "inline": compartilha a mesma árvore de
// rotas, mas acrescenta mws à stack de middleware inline acumulada até
// aqui (vazia se mx for o Mux raiz). Rotas registradas através do valor
// retornado têm esse middleware inline compilado no handler no momento
// do registro — o middleware da raiz (Use no Mux raiz) não é repetido
// aqui, porque já é aplicado uma única vez no nível do roteamento.
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

// Group cria um novo Router inline no mesmo caminho de rota, herdando o
// middleware inline acumulado até aqui — chamadas a Use() dentro de fn
// estendem essa cópia sem afetar o Mux original nem outros grupos
// irmãos.
func (mx *Mux) Group(fn func(r Router)) Router {
	im := mx.With()
	if fn != nil {
		fn(im)
	}
	return im
}

// Route cria um sub-Mux independente (árvore própria), executa fn nele e
// o monta em pattern via Mount.
func (mx *Mux) Route(pattern string, fn func(r Router)) Router {
	sub := NewRouter()
	if fn != nil {
		fn(sub)
	}
	mx.Mount(pattern, sub)
	return sub
}

// bakedHandler envolve h com o middleware inline deste Mux, quando
// houver (nunca com o middleware da raiz, que é aplicado uma única vez
// no nível do roteamento — ver Use/ServeHTTP).
func (mx *Mux) bakedHandler(h http.Handler) http.Handler {
	if mx.inline {
		return mx.middlewares.Handler(h)
	}
	return h
}

// Mount anexa outro http.Handler (tipicamente outro *Mux) ao longo de
// pattern. O handler montado recebe, via RouteContext(r.Context()).RoutePath,
// o path já relativo ao ponto de montagem — permitindo que um sub-router
// registre suas próprias rotas a partir de "/", independente de onde foi
// montado no router pai. Se h também implementar Routes (como qualquer
// *Mux implementa), Mux.Routes() no pai reflete h como Route.SubRoutes,
// preservando a hierarquia de mount para ferramentas de introspecção.
func (mx *Mux) Mount(pattern string, h http.Handler) {
	mountPattern := strings.TrimSuffix(pattern, "/")

	// Caso exato: requisição bate exatamente no ponto de montagem (ex.:
	// GET /admin quando montado em "/admin"). O sub-router vê "/".
	exactHandler := mx.bakedHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		RouteContext(r.Context()).RoutePath = "/"
		h.ServeHTTP(w, r)
	}))

	// Caso com sufixo: o que sobrou do path depois do ponto de montagem
	// já vem calculado pelo próprio matching do catch-all como uma fatia
	// da string original (routeRest, sem alocação) — ver tree.go find().
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

// handle registra h para o método mt em pattern, com o middleware inline
// deste Mux já compilado (composto uma única vez, aqui — não a cada
// requisição, e não incluindo o middleware da raiz, aplicado separadamente).
func (mx *Mux) handle(mt methodTyp, pattern string, h http.Handler) {
	mx.routesRegistered = true
	mx.tree.insert(mt, pattern, mx.bakedHandler(h))
}

// Handle registra h para todos os métodos HTTP suportados em pattern.
func (mx *Mux) Handle(pattern string, h http.Handler) {
	for mt := methodTyp(0); mt < numMethods; mt++ {
		mx.handle(mt, pattern, h)
	}
}

// HandleFunc é equivalente a Handle, aceitando http.HandlerFunc.
func (mx *Mux) HandleFunc(pattern string, h http.HandlerFunc) {
	mx.Handle(pattern, h)
}

// Method registra h para o método HTTP informado em pattern. Apenas os
// nove métodos padrão são suportados (ver methodIndex).
func (mx *Mux) Method(method, pattern string, h http.Handler) {
	mt, ok := methodIndex(method)
	if !ok {
		panic("router: unsupported HTTP method: " + method)
	}
	mx.handle(mt, pattern, h)
}

// MethodFunc é equivalente a Method, aceitando http.HandlerFunc.
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

// NotFound define o handler de resposta para paths sem rota casada. O
// padrão é http.NotFound.
func (mx *Mux) NotFound(h http.HandlerFunc) { mx.notFoundHandler = h }

// MethodNotAllowed define o handler de resposta para paths casados cujo
// método não está registrado. O padrão responde 405 com o header Allow
// preenchido.
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

// Routes retorna a árvore de roteamento em uma estrutura navegável, para
// introspecção. Sub-routers anexados via Mount aparecem com
// Route.SubRoutes preenchido (quando o handler montado também implementa
// Routes), preservando a hierarquia em vez de achatar tudo em uma lista.
func (mx *Mux) Routes() []Route {
	routes := mx.tree.routes()
	return append(routes, mx.mounts...)
}

// Middlewares retorna a lista de middlewares em uso neste Mux.
func (mx *Mux) Middlewares() Middlewares {
	return mx.middlewares
}

// Match busca na árvore um handler que case com method/path, sem
// executá-lo.
func (mx *Mux) Match(rctx *Context, method, path string) bool {
	mt, ok := methodIndex(method)
	if !ok {
		return false
	}
	n, _ := mx.tree.find(rctx, mt, path)
	return n != nil
}

// Find busca na árvore o pattern que casa com method/path.
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

// ServeHTTP implementa http.Handler. No Mux raiz, constrói (uma única
// vez, de forma segura para concorrência via sync.Once) o wrap de
// mx.middlewares ao redor de mx.routeHTTP — é esse wrap que faz
// Use()-level middleware rodar ANTES do roteamento em si, necessário
// para StripSlashes/RedirectSlashes e qualquer middleware que precise
// inspecionar ou alterar a requisição antes de saber qual rota vai casar.
//
// Um Mux inline (With/Group) nunca é servido diretamente em uso normal —
// mas caso seja, cai no mesmo caminho, aplicando apenas o middleware
// inline acumulado nele (sem o da raiz, que nesse caso nunca rodou).
func (mx *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mx.handlerOnce.Do(func() {
		mx.handler = mx.middlewares.Handler(http.HandlerFunc(mx.routeHTTP))
	})
	mx.handler.ServeHTTP(w, r)
}

// routeHTTP faz o trabalho de fato: obtém (ou cria, se for o Mux raiz na
// cadeia de mounts) o Context de roteamento, busca o node correspondente
// na árvore e invoca o handler já pré-composto armazenado nele.
//
// O Context, quando recém-criado, é usado diretamente como o
// context.Context da requisição (r.WithContext(rctx)) — como Context
// implementa a interface inteira (Deadline/Done/Err/Value), evitamos o
// wrapper extra que context.WithValue alocaria. O único alloc que resta
// no caminho feliz é o próprio r.WithContext (new(Request)), que é o piso
// teórico sem recorrer a unsafe — ver decisão registrada no histórico do
// projeto.
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
