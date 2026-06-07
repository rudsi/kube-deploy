package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"kube-deploy/internal/k8s"
	"kube-deploy/internal/model"
	"kube-deploy/internal/service"
)

// Server registers routes on a ServeMux and delegates to DeployService.
type Server struct {
	deployments *service.DeployService
	apiToken    string
}

// Option customizes the API server.
type Option func(*Server)

// WithAPIToken requires Authorization: Bearer <token> for protected endpoints.
func WithAPIToken(token string) Option {
	return func(s *Server) {
		s.apiToken = strings.TrimSpace(token)
	}
}

// NewServer creates the API server with the given deploy service.
func NewServer(deployments *service.DeployService, opts ...Option) *Server {
	s := &Server{deployments: deployments}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register wires all API endpoints (Go 1.22+ method-aware patterns).
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /health", s.healthz) // alias for VIBSL and other PaaS health checks
	mux.Handle("POST /deploy", s.requireAuth(http.HandlerFunc(s.deploy)))
	mux.Handle("GET /deployments", s.requireAuth(http.HandlerFunc(s.listDeployments)))
	mux.Handle("GET /deployments/{id}", s.requireAuth(http.HandlerFunc(s.getDeployment)))
}

// healthz is a simple liveness endpoint for the API process.
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// deploy accepts JSON, runs the full Kubernetes deploy pipeline, returns 201 or an error.
func (s *Server) deploy(w http.ResponseWriter, r *http.Request) {
	var req model.DeployRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, err)
		return
	}

	rec, err := s.deployments.Deploy(r.Context(), req)
	if err != nil {
		var he *httpError
		if mapped := MapValidationError(err); errors.As(mapped, &he) {
			writeError(w, mapped)
			return
		}
		// Cluster apply failures (permissions, quota, invalid spec) surface as 502.
		writeJSON(w, http.StatusBadGateway, model.ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, model.DeployResponse{Deployment: *rec})
}

// listDeployments returns every deployment tracked in this process's memory.
func (s *Server) listDeployments(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, model.DeploymentListResponse{
		Deployments: s.deployments.List(),
	})
}

// getDeployment returns one deployment by path id.
func (s *Server) getDeployment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, badRequest("deployment id is required"))
		return
	}
	rec, ok := s.deployments.Get(id)
	if !ok {
		writeError(w, notFound(fmt.Sprintf("deployment %q not found", id)))
		return
	}
	writeJSON(w, http.StatusOK, model.DeployResponse{Deployment: *rec})
}

// MapValidationError converts generator validation errors to HTTP 400.
func MapValidationError(err error) error {
	if err == nil {
		return nil
	}
	if k8s.IsValidationError(err) {
		return badRequest(err.Error())
	}
	return err
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	if s.apiToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="kube-deploy"`)
			writeError(w, unauthorized("missing or invalid bearer token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, ok := strings.Cut(auth, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(s.apiToken)) == 1
}
