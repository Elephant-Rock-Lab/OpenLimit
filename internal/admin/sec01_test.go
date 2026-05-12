package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// TEST-36-01-01: BearerAuth rejects wrong token via ConstantTimeCompare
// ---------------------------------------------------------------------------
func TestBearerAuth_RejectsWrongToken(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with wrong token")
	})

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("correct-admin-token", nil, nil, nil, inner))

	req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong token, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// TEST-36-01-02: BearerAuth accepts correct token via ConstantTimeCompare
// ---------------------------------------------------------------------------
func TestBearerAuth_AcceptsCorrectToken(t *testing.T) {
	called := false
	inner := http.NewServeMux()
	inner.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	authed := http.NewServeMux()
	authed.Handle("/", BearerAuth("correct-admin-token", nil, nil, nil, inner))

	req := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	req.Header.Set("Authorization", "Bearer correct-admin-token")
	w := httptest.NewRecorder()

	authed.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for correct token, got %d", w.Code)
	}
	if !called {
		t.Error("expected inner handler to be called")
	}
}
