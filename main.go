// kube-deploy is an HTTP API that accepts JSON deployment requests,
// builds Kubernetes manifests in Go, and applies them to a cluster via client-go.
//
// Author: Rudra
package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"kube-deploy/internal/api"
	"kube-deploy/internal/k8s"
	"kube-deploy/internal/service"
	"kube-deploy/internal/store"
)

// main wires dependencies (Kubernetes client, in-memory store, HTTP routes) and starts the server.
func main() {
	// Optional override; otherwise client-go loads KUBECONFIG or ~/.kube/config.
	kubeconfig := os.Getenv("KUBECONFIG_PATH")

	clientset, err := k8s.NewClientset(kubeconfig)
	if err != nil {
		log.Fatalf("kubernetes client: %v", err)
	}

	st := store.New()
	applier := k8s.NewApplier(clientset)
	applier.SetRolloutTimeout(durationEnv("ROLLOUT_TIMEOUT", 2*time.Minute))
	deploySvc := service.NewDeployService(st, applier)
	server := api.NewServer(deploySvc, api.WithAPIToken(os.Getenv("API_TOKEN")))

	mux := http.NewServeMux()
	server.Register(mux)

	host := envOrDefault("HOST", "127.0.0.1")
	port := envOrDefault("PORT", "8080")
	if !isLoopbackHost(host) && os.Getenv("API_TOKEN") == "" {
		log.Fatal("API_TOKEN is required when HOST is not loopback")
	}

	addr := net.JoinHostPort(host, port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	log.Printf("kube-deploy API listening on http://%s", addr)
	log.Printf("using kubeconfig: %s", kubeconfigDisplay(kubeconfig))
	if os.Getenv("API_TOKEN") == "" {
		log.Printf("API_TOKEN not set; API auth is disabled for loopback-only development")
	}
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// envOrDefault returns the environment variable value or a fallback when unset.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// kubeconfigDisplay describes which kubeconfig path is active (for startup logs only).
func kubeconfigDisplay(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("KUBECONFIG"); v != "" {
		return v
	}
	return "default (~/.kube/config)"
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Fatalf("%s must be a Go duration like 30s or 2m: %v", key, err)
	}
	return d
}

func isLoopbackHost(host string) bool {
	switch host {
	case "localhost":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
