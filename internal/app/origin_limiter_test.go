package app_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sidus.io/charge/internal/app"
)

func TestOriginLimiter_AcquireWithinLimit(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(5)

	assert.True(t, limiter.TryAcquire("http://example.com"))
}

func TestOriginLimiter_AcquireAtLimit(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(2)

	assert.True(t, limiter.TryAcquire("http://example.com"))
	assert.True(t, limiter.TryAcquire("http://example.com"))
	assert.False(t, limiter.TryAcquire("http://example.com"))
}

func TestOriginLimiter_ReleaseFreesSlot(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(1)

	assert.True(t, limiter.TryAcquire("http://example.com"))
	limiter.Release("http://example.com")
	assert.True(t, limiter.TryAcquire("http://example.com"))
}

func TestOriginLimiter_ReleaseNoopForUnacquired(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(5)

	assert.NotPanics(t, func() {
		limiter.Release("http://never-acquired.com")
	})
}

func TestOriginLimiter_PerOriginIsolation(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(1)

	assert.True(t, limiter.TryAcquire("http://alpha.com"))
	assert.False(t, limiter.TryAcquire("http://alpha.com"))

	assert.True(t, limiter.TryAcquire("http://beta.com"))
}

func TestOriginLimiter_ZeroLimit(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(0)

	assert.False(t, limiter.TryAcquire("http://example.com"))
}

func TestOriginLimiter_MiddlewareEmptyOrigin(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(1)

	assert.True(t, limiter.TryAcquire(""))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	called := false

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestOriginLimiter_MiddlewareAllows(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(5)

	called := false
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestOriginLimiter_MiddlewareBlocks(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(1)

	limiter.TryAcquire("http://example.com")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	called := false

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "5", rec.Header().Get("Retry-After"))
}

func TestOriginLimiter_MiddlewareReleaseOnCompletion(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(1)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	assert.True(t, limiter.TryAcquire("http://example.com"))
}

func TestOriginLimiter_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	limiter := app.NewOriginLimiter(10)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.TryAcquire("http://example.com") {
				limiter.Release("http://example.com")
			}
		}()
	}
	wg.Wait()
}
