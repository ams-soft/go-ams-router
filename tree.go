package router

import (
	"net/http"
	"strings"
)

// nodeType identifica o papel de um node dentro da árvore de roteamento.
type nodeType uint8

const (
	ntStatic   nodeType = iota // segmento literal: /users
	ntParam                    // segmento nomeado inteiro: /{id}
	ntCompound                 // segmento com params parciais: /{month}-{day}
	ntCatchAll                 // wildcard: /* — só é válido como último segmento
)

// node é um nó da árvore de roteamento. A árvore é organizada por
// segmento de path (dividido em '/'), com quatro tipos de node — não é
// um radix tree de bytes como o do chi/httprouter (que comprime prefixos
// byte a byte). Essa escolha deliberada troca uma fatia de performance de
// pico por bem menos superfície de bugs: o fan-out por nível tende a ser
// pequeno na prática, então o scan linear em children é rápido e não
// aloca. O caminho comum — um único {param} ocupando o segmento inteiro —
// continua zero-alloc; segmentos compostos (mistura de literal com
// {param}, ex. "{month}-{day}-{year}") também são zero-alloc, casados por
// varredura de delimitador (strings.Index) em vez de regexp — ver
// matchCompound.
type node struct {
	typ nodeType
	seg string // static: texto literal; param/catchAll: nome do parâmetro

	children []*node // filhos estáticos
	compound []*node // filhos com segmento composto (delimitador), testados em ordem de registro
	param    *node   // no máximo um filho param (segmento inteiro) por node
	catchAll *node   // no máximo um filho catch-all por node, sempre folha

	// compoundParts só é usado quando typ == ntCompound.
	compoundParts []compoundPart

	handlers [numMethods]http.Handler

	pattern      string // pattern completo registrado, usado por RoutePattern()/Routes()
	isMountPoint bool   // true para os nodes que Mux.Mount() cria — ver Mux.Routes()
}

// insert adiciona um handler para o método mt no pattern informado,
// criando nodes conforme necessário, e retorna o node final (usado por
// Mount para marcar isMountPoint). Chamado apenas no registro de rotas
// (startup), nunca no caminho de uma requisição — por isso não há
// preocupação de zero-alloc aqui.
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

// child encontra ou cria o filho de cur correspondente a seg, decidindo
// o tipo de node a partir do formato do segmento:
//   - "*"                          → catch-all
//   - "{name}" (segmento inteiro)  → param simples
//   - contém "{" mas não é só isso → compound, decomposto em literal+param
//   - qualquer outra coisa         → estático (literal exato)
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

// compoundPart é um pedaço de um segmento composto — texto literal ou um
// parâmetro nomeado — na ordem em que aparece no pattern.
type compoundPart struct {
	isParam bool
	text    string // literal: texto exato a casar; param: nome do parâmetro
}

// parseCompound decompõe um segmento como "{month}-{day}-{year}" em
// literais e parâmetros alternados, usado só para segmentos com {param}
// misturado com texto literal — nunca no caso comum de {param} sozinho
// (esse continua pelo caminho ntParam, sem passar por aqui).
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

// matchCompound casa seg contra parts em runtime, sem regexp: cada
// parâmetro consome o menor prefixo possível até o próximo literal (ou até
// o fim do segmento, se for o último part) — o mesmo critério de um grupo
// não-guloso (.+?), calculado com strings.Index em vez de um motor de
// regex, então fica zero-alloc. Não faz backtracking entre parâmetros: se
// o literal seguinte aparecer mais de uma vez em seg, usa a primeira
// ocorrência, que é o caso comum (delimitadores fixos tipo "-" ou ".").
// Só grava em rctx.params quando o segmento inteiro casa — sem side
// effects em caso de falha.
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
			// último part: consome o resto do segmento.
			if pos >= len(seg) {
				return false
			}
			end = len(seg)

		case !parts[pi+1].isParam:
			// delimitado pelo literal seguinte.
			idx := strings.Index(seg[pos:], parts[pi+1].text)
			if idx <= 0 { // -1: sem delimitador; 0: param vazio (.+ exige >=1 char)
				return false
			}
			end = pos + idx

		default:
			// param seguido de outro param sem literal entre eles: caso
			// degenerado e não documentado, cada um consome 1 char (o
			// mínimo que um (.+?) sem constraint capturaria).
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

// find percorre a árvore buscando o node cujo pattern casa com method/path.
// Os parâmetros encontrados no caminho — inclusive os de segmentos
// compostos, via matchCompound — são gravados diretamente em rctx.params
// (array de tamanho fixo, sem slice dinâmico), então o matching inteiro é
// zero-alloc, não só o caminho estático/param/catch-all.
//
// Trailing slash é significativo: "/foo" e "/foo/" só casam com o mesmo
// handler se ambos os patterns tiverem sido registrados explicitamente
// (ou se StripSlashes/RedirectSlashes normalizar o path antes do
// roteamento — ver pacote middleware). Catch-all é a exceção: ele casa
// tanto com quanto sem barra final, por ser sua natureza capturar "o
// resto do path", com ou sem conteúdo.
//
// Retorno:
//   - (node, nil)      quando há handler para mt no pattern casado
//   - (nil, allowed)   quando o pattern casa mas não para o método mt
//     (allowed lista os métodos que casam — único ponto desta função que
//     aloca fora do caso de segmento composto, e só no caminho de erro
//     405, não no caminho feliz)
//   - (nil, nil)       quando nenhum pattern casa (404)
func (n *node) find(rctx *Context, mt methodTyp, path string) (*node, []string) {
	if path == "/" {
		return n.finish(mt)
	}

	cur := n
	i := 1 // pula a barra inicial
	ln := len(path)

	for i <= ln {
		start := i
		for i < ln && path[i] != '/' {
			i++
		}
		seg := path[start:i]
		trailingEmpty := seg == "" && i == ln

		if seg == "" && !trailingEmpty {
			// barra dupla no meio do path: tolera e segue.
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
			// path[start-1:] inclui a barra que antecede o segmento —
			// fatia da mesma string (sem alocar), usada internamente
			// por Mount. Quando trailingEmpty, start==ln e path[start-1:]
			// é só "/", que é o RoutePath correto pro sub-router.
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

// finish resolve o handler final de n para o método mt, ou monta a lista
// de métodos permitidos para uma resposta 405.
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

// splitPattern divide um pattern em segmentos. Uma barra final explícita
// (exceto para o pattern raiz "/") produz um segmento vazio adicional no
// final, preservando a distinção entre "/foo" e "/foo/" — ver find()
// para como isso é casado contra o path real da requisição.
func splitPattern(pattern string) []string {
	body := strings.TrimPrefix(pattern, "/")
	if body == "" {
		return nil // pattern raiz "/": zero segmentos
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

// routes coleta recursivamente todas as rotas registradas a partir deste
// node, para introspecção (Mux.Routes()). Nodes marcados como ponto de
// mount são pulados aqui — Mux.Routes() os representa separadamente,
// como Route.SubRoutes, a partir de Mux.mounts. Não é usado no hot path
// de requisições, então alocar aqui é aceitável.
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
