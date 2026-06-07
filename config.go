package main

import (
	"log"
	"os"
	"strings"
)

// resolveAPIToken reads the bearer token from env or an optional file path.
func resolveAPIToken() string {
	for _, key := range []string{"API_TOKEN", "KUBE_DEPLOY_API_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	if path := strings.TrimSpace(os.Getenv("API_TOKEN_FILE")); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			log.Printf("warning: could not read API_TOKEN_FILE %q: %v", path, err)
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	return ""
}

func logStartupConfig(host, port, token string) {
	_, inDocker := os.LookupEnv("PORT")
	dockerEnv := false
	if _, err := os.Stat("/.dockerenv"); err == nil {
		dockerEnv = true
	}
	log.Printf(
		"startup: env HOST=%q PORT=%q | bind=%s:%s cloud=%t container=%t | API_TOKEN configured=%t",
		os.Getenv("HOST"),
		os.Getenv("PORT"),
		host,
		port,
		isCloudRuntime(),
		dockerEnv || inDocker,
		token != "",
	)
}
