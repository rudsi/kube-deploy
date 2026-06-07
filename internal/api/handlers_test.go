package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProtectedEndpointsRequireBearerToken(t *testing.T) {
	server := NewServer(nil, WithAPIToken("secret"))
	mux := http.NewServeMux()
	server.Register(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/deployments", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /deployments status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", rr.Code, http.StatusOK)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestProtectedEndpointsReturn503WhenAuthEnforcedWithoutToken(t *testing.T) {
	server := NewServer(nil, WithAuthEnforced(true))
	mux := http.NewServeMux()
	server.Register(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/deployments", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /deployments status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestDeployRejectsOversizedBody(t *testing.T) {
	server := NewServer(nil)
	mux := http.NewServeMux()
	server.Register(mux)

	body := `{"appName":"` + strings.Repeat("a", maxRequestBodyBytes) + `"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deploy", strings.NewReader(body))
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("POST /deploy status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
