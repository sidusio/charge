package app

import (
	"net/http"

	"sidus.io/charge/internal/util"
)

type OriginGuard struct {
	allowed  util.SyncMap[string, struct{}]
	allowAll bool
}

func NewOriginGuard(allowedOrigins []string) *OriginGuard {
	og := OriginGuard{
		allowAll: len(allowedOrigins) == 0,
	}
	for _, origin := range allowedOrigins {
		og.allowed.Store(origin, struct{}{})
	}

	return &og
}

func (og *OriginGuard) IsAllowed(origin string) bool {
	if og.allowAll {
		return true
	}

	if _, ok := og.allowed.Load(origin); ok {
		return true
	}

	return false
}

func (og *OriginGuard) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("origin")
		if !og.IsAllowed(origin) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
