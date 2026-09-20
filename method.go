package router

import "net/http"

// methodTyp identifica um método HTTP como índice de array, permitindo
// dispatch via handlers[methodTyp] em vez de um lookup em map[string]Handler
// por requisição.
type methodTyp int8

const (
	mCONNECT methodTyp = iota
	mDELETE
	mGET
	mHEAD
	mOPTIONS
	mPATCH
	mPOST
	mPUT
	mTRACE
	numMethods // sentinela: número total de métodos suportados
)

// methodIndex resolve uma string de método HTTP para seu índice de array.
// O switch é compilado pelo Go como uma sequência eficiente de comparações,
// sem alocação e sem o custo de hashing de um map.
func methodIndex(method string) (methodTyp, bool) {
	switch method {
	case http.MethodConnect:
		return mCONNECT, true
	case http.MethodDelete:
		return mDELETE, true
	case http.MethodGet:
		return mGET, true
	case http.MethodHead:
		return mHEAD, true
	case http.MethodOptions:
		return mOPTIONS, true
	case http.MethodPatch:
		return mPATCH, true
	case http.MethodPost:
		return mPOST, true
	case http.MethodPut:
		return mPUT, true
	case http.MethodTrace:
		return mTRACE, true
	default:
		return 0, false
	}
}

// methodName é o inverso de methodIndex, usado para montar o header Allow
// em respostas 405 Method Not Allowed.
func methodName(mt methodTyp) string {
	switch mt {
	case mCONNECT:
		return http.MethodConnect
	case mDELETE:
		return http.MethodDelete
	case mGET:
		return http.MethodGet
	case mHEAD:
		return http.MethodHead
	case mOPTIONS:
		return http.MethodOptions
	case mPATCH:
		return http.MethodPatch
	case mPOST:
		return http.MethodPost
	case mPUT:
		return http.MethodPut
	case mTRACE:
		return http.MethodTrace
	default:
		return ""
	}
}
