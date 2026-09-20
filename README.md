# go-router

Um router HTTP leve, idiomático e composável para construir serviços em Go —
alternativa ao [chi](https://github.com/go-chi/chi), com foco em **zero
allocations no hot path** sem abrir mão da compatibilidade total com
`net/http`.

- ⚡️ **Fast** — árvore de roteamento em trie por segmento + dispatch de
  método via array fixo, sem `map[string]http.Handler` no caminho da
  requisição.
- 🔥 **Robust** — 32 testes (incluindo `-race`), cobrindo rotas estáticas,
  parâmetros, segmentos compostos, catch-all, trailing slash estrito,
  `Mount`/`Route` aninhados e ordenação de middleware.
- 📼 **Zero dependências externas** — só stdlib.
- 🚀 **Lightweight** — núcleo pequeno, uma responsabilidade por arquivo.
- 🧊 **1 alloc/op** em todo caminho de roteamento — estático, param,
  múltiplos params, segmento composto, middleware encadeado e `Mount`
  aninhado. É o piso teórico para qualquer router 100% compatível com
  `http.Handler` sem recorrer a `unsafe` (ver
  [Arquitetura](#arquitetura--decisões-de-design)).

## Instalação

```
go get github.com/ams-soft/go-ams-router
```

Requer Go 1.27+ (ver [Por que Go 1.27](#por-que-go-127)).

## Uso rápido

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

Veja `_examples/basic/main.go` para um exemplo mais completo, incluindo
`Group`, `Route`+`Mount` e wildcard.

## API

A interface `Router` cobre o mesmo conjunto de operações do chi:
`Use`, `With`, `Group`, `Route`, `Mount`, `Handle`/`HandleFunc`,
`Method`/`MethodFunc`, um método por verbo HTTP padrão (`Get`, `Post`,
`Put`, `Patch`, `Delete`, `Head`, `Options`, `Connect`, `Trace`),
`NotFound` e `MethodNotAllowed`. `URLParam(r, key)` e
`URLParamFromCtx(ctx, key)` lêem parâmetros de rota; `URLParam(r, "*")` lê
o valor capturado por um catch-all (`/files/*`).

O subpacote `middleware` (mesmo módulo — ver
[Estrutura do repositório](#estrutura-do-repositório)) traz `Logger`,
`Recoverer`, `RequestID`, `Timeout`, `StripSlashes` e `RedirectSlashes`.

## Trailing slash é estrito por padrão

`/user/{name}` e `/user/{name}/` são rotas **diferentes** — cada uma
precisa ser registrada explicitamente para responder. Isso é intencional
(evita ambiguidade silenciosa sobre qual variante o cliente pretendia) e
é o mesmo comportamento padrão do chi.

Catch-all é a exceção deliberada: `/files/*` casa tanto `/files/a/b`
quanto `/files/a/b/`, porque a natureza de um wildcard é capturar "o
resto do path", com ou sem conteúdo depois da barra.

Quem preferir tolerância (tratar `/foo` e `/foo/` como a mesma rota) usa
um dos dois middlewares, registrados com `Use()` **no Mux raiz**
(precisam rodar antes do roteamento decidir qual rota casa — ver
[Arquitetura](#arquitetura--decisões-de-design)):

```go
r := router.NewRouter()
r.Use(middleware.StripSlashes)   // normaliza silenciosamente
// ou:
r.Use(middleware.RedirectSlashes) // responde 301 removendo a barra final
```

## Segmentos compostos (múltiplos params em um segmento)

Além do caso comum — `{id}` ocupando o segmento inteiro — patterns com
`{param}` misturado a texto literal no mesmo segmento também são
suportados:

```go
r.Get("/articles/{month}-{day}-{year}", handler)
// GET /articles/01-16-2017 → month=01, day=16, year=2017
```

O pattern é decomposto em literal+param uma vez no registro da rota; em
runtime, cada param é casado por `strings.Index` contra o próximo literal
(sem `regexp`), então segmento composto é tão zero-alloc quanto `{id}`
sozinho (ver benchmarks abaixo).

## Arquitetura & decisões de design

### O piso teórico de allocations com `http.Handler`

Qualquer router que mantenha a assinatura padrão `func(w
http.ResponseWriter, r *http.Request)` — em vez de uma assinatura própria
como `func(w, r, params)` — precisa anexar os parâmetros de rota através
de `r.WithContext(ctx)`, porque não há outro lugar padrão para colocá-los.
`Request.WithContext` sempre aloca um `*Request` novo
(`r2 := new(Request)`). Esse é o piso: **1 alloc/op**, incontornável sem
recorrer a `unsafe` para escrever no campo não-exportado `Request.ctx`
diretamente (deliberadamente descartado neste projeto — o ganho não
compensa o risco de quebrar em cada mudança de versão do Go).

Os benchmarks abaixo confirmam que o router bate esse piso de forma
consistente, mesmo em rotas com múltiplos parâmetros, middleware
encadeado, `Mount` aninhado e sob `-race`:

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

(máquina de referência: Intel i9-12900HX, Go 1.27.1, linux/amd64 — os
números absolutos variam por hardware, o que importa é o **1 allocs/op**
constante em todo caminho, inclusive segmento composto (`{month}-{day}-{year}`),
casado por varredura de delimitador em vez de regexp — ver `matchCompound`
em `tree.go`. Reproduza com os comandos acima.)

Como isso é alcançado:

1. **`Context` implementa `context.Context` manualmente** (`Deadline`,
   `Done`, `Err`, `Value`) em vez de usar `context.WithValue` — que
   alocaria um `*valueCtx` adicional por cima do `WithContext`. `Context`
   é reciclado via `sync.Pool` entre requisições.
2. **Parâmetros em array fixo** (`[8]string` para chaves e valores), não
   slice dinâmico — sem alocação de slice por requisição enquanto o
   número de params ficar dentro do limite (`maxParams`, hoje 8).
3. **Dispatch de método por array** (`[9]http.Handler` por node),
   resolvido por `switch` sobre a string do método — sem hashing de map.
4. **Middleware compilado no registro da rota, não por requisição.** Há
   duas camadas: o middleware do Mux raiz (via `Use()`) envolve o
   roteamento inteiro — composto uma única vez via `sync.Once`, na
   primeira requisição, e roda ANTES do match na árvore (é o que permite
   `StripSlashes` alterar o path antes do roteamento decidir a rota); o
   middleware inline (via `With()`/`Group()`) é compilado diretamente no
   handler de cada rota, no momento do registro. Nenhuma composição de
   closures acontece por requisição em nenhum dos dois casos, e nenhum
   dos dois é executado em duplicidade (o inline nunca inclui uma cópia
   do que a raiz já cobre).
5. **Mount sem concatenação de string**: o restante do path depois do
   ponto de montagem é obtido via *slice* da string original
   (`path[start-1:]`), não via concatenação (`"/" + valor`) — slicing de
   string em Go não aloca porque compartilha o array de bytes
   subjacente; concatenação sim.

### Árvore de roteamento: trie por segmento, não radix tree de bytes

A árvore é organizada por **segmento de path** (dividido em `/`), com
quatro tipos de node — estático, parâmetro nomeado (`{id}`), composto
(`{a}-{b}`, decomposto em literal+param, sem regexp) e catch-all (`*`) —
em vez de um radix tree de
bytes como o do chi/httprouter (que comprime prefixos byte a byte). Essa
escolha deliberada troca uma fatia de performance de pico por muito menos
superfície de bugs: o fan-out por nível tende a ser pequeno na prática
(poucos recursos irmãos sob um mesmo prefixo), então o scan linear em
`children` é rápido e não aloca.

**Limitação conhecida, documentada para quem for estender o projeto:**
apenas os 9 métodos HTTP padrão são suportados (ver `method.go`) — sem
um mecanismo de registro de métodos customizados (ex.: `PURGE`, `LOCK`).
Quem precisar disso pode adicionar um node tipo "catch-all de método" ou
um fallback baseado em `map[string]http.Handler` por node, isolado do
array fixo dos 9 padrão para não pagar custo extra em quem não usa.

### Por que não FFI/Rust

Foi cogitado e descartado: o overhead de uma chamada `cgo` (100-200ns,
troca de stack, impossibilidade de preempção normal do goroutine) é maior
que o tempo total do matching de path que se pretendia acelerar, e
quebraria zero-dependências, cross-compilation trivial (`GOOS`/`GOARCH`
sem toolchain extra) e o modelo de concorrência do Go sob carga alta.

## Estrutura do repositório

Single-module: um `go.mod` só, na raiz, cobrindo o core e o subpacote
`middleware`. Essa escolha foi deliberada para a fase atual do projeto —
o acoplamento entre mudanças no core e nos middlewares essenciais durante
o desenvolvimento é alto o suficiente para que versionamento conjunto
seja mais simples e seguro do que multi-module (múltiplos `go.mod`
via `go.work`). Extrair `middleware/` (ou outros pacotes satélite futuros
como `render`/`docgen`, no espírito do que o chi faz como repos
separados) para um módulo próprio é uma migração viável mais adiante, não
uma decisão definitiva.

```
go-router/
├── go.mod
├── LICENSE
├── router.go                        // interfaces Router, Routes, Route, Middlewares, Walk
├── context.go                       // Context (implementa context.Context), pool, routeParams
├── params.go                        // URLParam / URLParamFromCtx (nível de pacote)
├── method.go                        // dispatch de método via array (9 métodos padrão)
├── tree.go                          // árvore de roteamento (trie por segmento + compound sem regex)
├── mux.go                           // Mux: implementação concreta de Router + ServeHTTP
├── router_test.go                   // testes de correção
├── router_bench_test.go             // benchmarks (allocs/op)
├── race_test.go                     // teste de concorrência na primeira requisição (-race)
├── strip_slashes_integration_test.go // StripSlashes/RedirectSlashes + roteamento estrito
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
    └── basic/main.go                // prefixo "_" — ignorado por go build/test ./...
```

## Por que Go 1.27

- **Alocação de memória "size-specialized"**: o compilador gera chamadas
  para rotinas de alocação especializadas por tamanho, reduzindo o custo
  de alocações pequenas (<80 bytes) em até 30% — o perfil exato do
  `*Context` e do `*http.Request` que este router aloca por requisição.
- **Métodos genéricos**: permitem, no futuro, uma camada opcional
  tipada por cima do core (ex.: binding automático de params para
  struct) sem contaminar a assinatura `http.Handler` do core.
- **Pacote `uuid` na stdlib**: possibilita adicionar um matcher de tipo
  `{id:uuid}` no futuro sem quebrar o requisito de zero dependências
  externas.

## Rodando os testes

```
go test ./...                       # correção
go test -race ./...                 # concorrência
go test -run=^$ -bench=. -benchmem ./...  # performance (allocs/op)
go vet ./...
```

## Roadmap / extensões possíveis

- [ ] Matcher de tipo (`{id:int}`, `{id:uuid}`) usando o pacote `uuid` da
      stdlib do Go 1.27
- [ ] Camada opcional de generics por cima do core (métodos genéricos,
      Go 1.27+) para binding tipado de params
- [ ] Suporte a métodos HTTP customizados (`PURGE`, `LOCK`, etc.), via
      map lazy por node, isolado do array fixo dos 9 métodos padrão

## Licença

MIT — ver [LICENSE](LICENSE).
