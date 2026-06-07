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
	applier := k8s.NewApplier(clientset)
	if err != nil {
		log.Printf("warning: kubernetes client unavailable: %v", err)
		log.Printf("API will start, but POST /deploy returns 502 until cluster credentials are configured")
		applier = k8s.NewApplier(nil)
	}
	applier.SetRolloutTimeout(durationEnv("ROLLOUT_TIMEOUT", 2*time.Minute))

	st := store.New()
	deploySvc := service.NewDeployService(st, applier)
	server := api.NewServer(deploySvc, api.WithAPIToken(os.Getenv("API_TOKEN")))

	mux := http.NewServeMux()
	server.Register(mux)

	host := listenHost()
	port := envOrDefault("PORT", "8080")
	if !isLoopbackHost(host) && os.Getenv("API_TOKEN") == "" {
		log.Fatal("API_TOKEN is required when HOST is not loopback (set a secret token in your platform env vars)")
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
	if token := os.Getenv("API_TOKEN"); token == "" {
		log.Printf("API_TOKEN not set; API auth is disabled for loopback-only development")
	} else {
		log.Printf("API_TOKEN is set; protected endpoints require Authorization: Bearer <token>")
	}
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// listenHost picks the bind address. Container/PaaS runtimes (VIBSL, etc.) bind 0.0.0.0
// so platform health checks reach the process; local dev defaults to loopback-only.
func listenHost() string {
	if v := os.Getenv("HOST"); v != "" {
		return v
	}
	if isCloudRuntime() {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

func isCloudRuntime() bool {
	for _, key := range []string{
		"VIBSL_ENVIRONMENT",
		"VIBSL_APP_ID",
		"VIBSL_DEPLOYMENT_ID",
		"WEBSITE_HOSTNAME", // Azure App Service / VIBSL BYOC
		"RENDER",
		"FLY_APP_NAME",
		"K_SERVICE",
		"DYNO",
	} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	// VIBSL and similar platforms run inside a container even when PORT is unset at boot.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Platforms inject PORT; local `go run` / `.\bin\kube-deploy.exe` usually does not.
	return os.Getenv("PORT") != ""
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
