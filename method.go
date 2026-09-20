package router

import "net/http"

// methodTyp identifies an HTTP method as an array index, enabling
// dispatch via handlers[methodTyp] instead of a map[string]Handler
// lookup per request.
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
	numMethods // sentinel: total number of supported methods
)

// methodIndex resolves an HTTP method string to its array index. The
// switch is compiled by Go into an efficient sequence of comparisons,
// with no allocation and no map hashing cost.
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

// methodName is the inverse of methodIndex, used to build the Allow
// header in 405 Method Not Allowed responses.
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
