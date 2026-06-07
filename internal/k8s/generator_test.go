package k8s

import (
	"testing"
	"time"

	"kube-deploy/internal/model"
)

const testDeploymentID = "12345678-1234-1234-1234-123456789abc"

func TestGenerateReturnsValidationErrorForBadQuantity(t *testing.T) {
	req := validDeployRequest()
	req.CPURequest = "not-a-quantity"

	_, _, err := Generate(testDeploymentID, req, time.Unix(0, 0).UTC())
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

func TestGenerateRejectsInvalidReplicas(t *testing.T) {
	req := validDeployRequest()
	req.Replicas = -1

	_, _, err := Generate(testDeploymentID, req, time.Unix(0, 0).UTC())
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

func TestGenerateUsesStableSelectorInstance(t *testing.T) {
	req := validDeployRequest()

	bundle, _, err := Generate(testDeploymentID, req, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	got := bundle.Deployment.Spec.Selector.MatchLabels[labelInstance]
	if got != "demo-api" {
		t.Fatalf("selector instance = %q, want stable app name", got)
	}
}

func TestNetworkPolicyAllowsIngressControllerNamespace(t *testing.T) {
	req := validDeployRequest()

	bundle, _, err := Generate(testDeploymentID, req, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	for _, rule := range bundle.NetworkPolicy.Spec.Ingress {
		for _, peer := range rule.From {
			if peer.NamespaceSelector == nil {
				continue
			}
			if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == ingressControllerNamespace {
				return
			}
		}
	}
	t.Fatalf("network policy does not allow namespace %q", ingressControllerNamespace)
}

func validDeployRequest() model.DeployRequest {
	return model.DeployRequest{
		AppName:       "demo-api",
		Image:         "ghcr.io/nginxinc/nginx-unprivileged:1.27",
		Port:          8080,
		Replicas:      2,
		ProbePath:     "/",
		CPURequest:    "100m",
		CPULimit:      "500m",
		MemoryRequest: "128Mi",
		MemoryLimit:   "256Mi",
	}
}
