package service

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"kube-deploy/internal/k8s"
	"kube-deploy/internal/model"
	"kube-deploy/internal/store"
)

func TestDeployDoesNotPersistInvalidRequest(t *testing.T) {
	st := store.New()
	applier := k8s.NewApplier(fake.NewSimpleClientset())
	applier.SetRolloutTimeout(0)
	svc := NewDeployService(st, applier)

	_, err := svc.Deploy(context.Background(), model.DeployRequest{
		AppName:    "demo-api",
		Image:      "ghcr.io/nginxinc/nginx-unprivileged:1.27",
		Port:       8080,
		CPURequest: "not-a-quantity",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !k8s.IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if got := st.List(); len(got) != 0 {
		t.Fatalf("stored deployments = %d, want 0", len(got))
	}
}
