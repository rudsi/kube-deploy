package k8s

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"kube-deploy/internal/model"
)

func TestApplyIsIdempotentForExistingResources(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	applier := NewApplier(client)
	applier.SetRolloutTimeout(0)

	req := model.DeployRequest{
		AppName:  "demo-api",
		Image:    "ghcr.io/nginxinc/nginx-unprivileged:1.27",
		Port:     8080,
		Replicas: 1,
	}
	Normalize(&req)

	first, _, err := Generate(testDeploymentID, req, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("generate first bundle: %v", err)
	}
	if err := applier.Apply(ctx, first); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	req.ConfigData = map[string]string{"FEATURE_FLAG": "on"}
	secondID := "87654321-1234-1234-1234-123456789abc"
	second, _, err := Generate(secondID, req, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatalf("generate second bundle: %v", err)
	}
	if err := applier.Apply(ctx, second); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	configMaps, err := client.CoreV1().ConfigMaps(req.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list configmaps: %v", err)
	}
	if len(configMaps.Items) != 1 {
		t.Fatalf("configmap count = %d, want 1", len(configMaps.Items))
	}
	if got := configMaps.Items[0].Data["FEATURE_FLAG"]; got != "on" {
		t.Fatalf("updated configmap FEATURE_FLAG = %q, want on", got)
	}

	deployment, err := client.AppsV1().Deployments(req.Namespace).Get(ctx, req.AppName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if got := deployment.Annotations[annotationDeploymentID]; got != secondID {
		t.Fatalf("deployment id annotation = %q, want %q", got, secondID)
	}
}
