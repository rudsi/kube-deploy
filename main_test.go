package main

import "testing"

func TestListenHost(t *testing.T) {
	for _, key := range []string{
		"HOST", "PORT", "VIBSL_ENVIRONMENT", "VIBSL_APP_ID",
		"VIBSL_DEPLOYMENT_ID", "WEBSITE_HOSTNAME", "RENDER", "FLY_APP_NAME",
	} {
		t.Setenv(key, "")
	}

	if got := listenHost(); got != "127.0.0.1" {
		t.Fatalf("listenHost() = %q, want 127.0.0.1", got)
	}

	t.Setenv("VIBSL_ENVIRONMENT", "production")
	if got := listenHost(); got != "0.0.0.0" {
		t.Fatalf("listenHost() on VIBSL = %q, want 0.0.0.0", got)
	}

	t.Setenv("VIBSL_ENVIRONMENT", "")
	t.Setenv("PORT", "8080")
	if got := listenHost(); got != "0.0.0.0" {
		t.Fatalf("listenHost() with PORT set = %q, want 0.0.0.0", got)
	}

	t.Setenv("PORT", "")
	t.Setenv("HOST", "10.0.0.5")
	if got := listenHost(); got != "10.0.0.5" {
		t.Fatalf("listenHost() with HOST set = %q, want 10.0.0.5", got)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost"} {
		if !isLoopbackHost(host) {
			t.Fatalf("isLoopbackHost(%q) = false, want true", host)
		}
	}
	if isLoopbackHost("0.0.0.0") {
		t.Fatal("isLoopbackHost(0.0.0.0) = true, want false")
	}
}
