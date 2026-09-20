package router

import (
	"context"
	"net/http"
)

// URLParam retorna o parâmetro de URL de um http.Request, ou string vazia
// se a chave não existir ou não houver Context de roteamento associado.
func URLParam(r *http.Request, key string) string {
	if rc := RouteContext(r.Context()); rc != nil {
		return rc.URLParam(key)
	}
	return ""
}

// URLParamFromCtx retorna o parâmetro de URL a partir de um context.Context
// de requisição diretamente, útil dentro de middlewares que só têm acesso
// ao ctx (não ao *http.Request completo).
func URLParamFromCtx(ctx context.Context, key string) string {
	if rc := RouteContext(ctx); rc != nil {
		return rc.URLParam(key)
	}
	return ""
}
