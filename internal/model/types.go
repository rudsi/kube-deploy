// Package model defines JSON request/response shapes and deployment status constants.
package model

import "time"

// Deployment lifecycle values stored in memory and returned by the API.
const (
	StatusPending  = "pending"
	StatusApplying = "applying"
	StatusDeployed = "deployed"
	StatusFailed   = "failed"

	// ManagedBy is the app.kubernetes.io/managed-by label value on generated resources.
	ManagedBy = "kube-deploy"
)

// DeployRequest is the JSON body for POST /deploy.
type DeployRequest struct {
	AppName          string            `json:"appName"`
	Namespace        string            `json:"namespace,omitempty"`
	Image            string            `json:"image"`
	Port             int32             `json:"port"`
	Replicas         int32             `json:"replicas,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	ConfigData       map[string]string `json:"configData,omitempty"`
	CPURequest       string            `json:"cpuRequest,omitempty"`
	CPULimit         string            `json:"cpuLimit,omitempty"`
	MemoryRequest    string            `json:"memoryRequest,omitempty"`
	MemoryLimit      string            `json:"memoryLimit,omitempty"`
	ProbePath        string            `json:"probePath,omitempty"`
	IngressHost      string            `json:"ingressHost,omitempty"`
	IngressClassName string            `json:"ingressClassName,omitempty"`
}

// DeploymentRecord tracks one deploy operation (in-memory until the API restarts).
type DeploymentRecord struct {
	ID          string        `json:"id"`
	Status      string        `json:"status"`
	Request     DeployRequest `json:"request"`
	Namespace   string        `json:"namespace"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
	Resources   []string      `json:"resources,omitempty"`
	IngressHost string        `json:"ingressHost,omitempty"`
	Error       string        `json:"error,omitempty"`
}

// DeployResponse wraps a single deployment for GET /deployments/{id} and POST /deploy.
type DeployResponse struct {
	Deployment DeploymentRecord `json:"deployment"`
}

// DeploymentListResponse is the payload for GET /deployments.
type DeploymentListResponse struct {
	Deployments []DeploymentRecord `json:"deployments"`
}

// ErrorResponse is the standard JSON error body.
type ErrorResponse struct {
	Error string `json:"error"`
}
