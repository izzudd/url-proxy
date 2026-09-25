package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func TestDashboardBasicAuth(t *testing.T) {
	r := chi.NewRouter()
	r.Group(func(admin chi.Router) {
		admin.Use(middleware.BasicAuth("Test Realm", map[string]string{
			"admin": "secret123",
		}))
		admin.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		})
		admin.Post("/api/proxy", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"registered"}`))
		})
	})

	// 1. Without Auth
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized for dashboard, got %d", rr.Code)
	}

	reqPostNoAuth := httptest.NewRequest(http.MethodPost, "/api/proxy", nil)
	rrPostNoAuth := httptest.NewRecorder()
	r.ServeHTTP(rrPostNoAuth, reqPostNoAuth)
	if rrPostNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized for POST /api/proxy, got %d", rrPostNoAuth.Code)
	}

	// 2. With Wrong Credentials
	reqWrong := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	reqWrong.SetBasicAuth("admin", "wrongpass")
	rrWrong := httptest.NewRecorder()
	r.ServeHTTP(rrWrong, reqWrong)

	if rrWrong.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 with bad pass, got %d", rrWrong.Code)
	}

	// 3. With Correct Credentials
	reqValid := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	reqValid.SetBasicAuth("admin", "secret123")
	rrValid := httptest.NewRecorder()
	r.ServeHTTP(rrValid, reqValid)

	if rrValid.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rrValid.Code)
	}
}
