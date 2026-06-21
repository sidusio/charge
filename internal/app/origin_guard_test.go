package app_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"sidus.io/charge/internal/app"
)

func TestOriginGuard_AllowAll(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard(nil)

	assert.True(t, guard.IsAllowed("http://evil.com"))
	assert.True(t, guard.IsAllowed("http://example.com"))
	assert.True(t, guard.IsAllowed(""))
}

func TestOriginGuard_AllowsConfiguredOrigin(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard([]string{"http://example.com"})

	assert.True(t, guard.IsAllowed("http://example.com"))
}

func TestOriginGuard_RejectsUnknownOrigin(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard([]string{"http://example.com"})

	assert.False(t, guard.IsAllowed("http://evil.com"))
}

func TestOriginGuard_MultipleOrigins(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard([]string{
		"http://alpha.com",
		"http://beta.com",
		"http://gamma.com",
	})

	assert.True(t, guard.IsAllowed("http://alpha.com"))
	assert.True(t, guard.IsAllowed("http://beta.com"))
	assert.True(t, guard.IsAllowed("http://gamma.com"))
	assert.False(t, guard.IsAllowed("http://delta.com"))
}

func TestOriginGuard_EmptyOriginWhenRestricted(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard([]string{"http://example.com"})

	assert.False(t, guard.IsAllowed(""))
}

func TestOriginGuard_EmptyOriginWhenAllowAll(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard(nil)

	assert.True(t, guard.IsAllowed(""))
}

func TestOriginGuard_MiddlewarePassesThrough(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard([]string{"http://example.com"})

	called := false
	handler := guard.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOriginGuard_MiddlewareRejects(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard([]string{"http://example.com"})

	called := false
	handler := guard.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "", rec.Body.String())
}

func TestOriginGuard_MiddlewareAllowAll(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard(nil)

	called := false
	handler := guard.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://anything.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOriginGuard_MiddlewareAllowAllNoOriginHeader(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard(nil)

	called := false
	handler := guard.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOriginGuard_NoOriginHeader(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard([]string{"http://example.com"})

	called := false
	handler := guard.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOriginGuard_AllowEmptyOriginExplicitly(t *testing.T) {
	t.Parallel()

	guard := app.NewOriginGuard([]string{""})

	assert.True(t, guard.IsAllowed(""))
	assert.False(t, guard.IsAllowed("http://example.com"))
}
