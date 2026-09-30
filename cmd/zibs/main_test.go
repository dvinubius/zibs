package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeShutsDownWhenContextIsCanceled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, server, listener, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatalf("request to running server: %v", err)
	}
	response.Body.Close()

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not shut down within one second")
	}
}

func TestRunLogsLifecycle(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := run(ctx, "127.0.0.1:0", "127.0.0.1:0", filepath.Join(t.TempDir(), "test.db"), testAdminToken, logger)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, want := range []string{
		`msg="server started"`,
		`msg="metrics server started"`,
		`msg="shutdown signal received"`,
		`msg="server stopped"`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log = %q, want it to contain %q", logs.String(), want)
		}
	}
}

func TestRunRequiresAdminToken(t *testing.T) {
	err := run(
		context.Background(),
		"127.0.0.1:0",
		"127.0.0.1:0",
		filepath.Join(t.TempDir(), "test.db"),
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err == nil || !strings.Contains(err.Error(), adminTokenEnvironmentVariable) {
		t.Fatalf("run error = %v, want missing %s error", err, adminTokenEnvironmentVariable)
	}
}

func TestMetricsAddressFromEnvironment(t *testing.T) {
	t.Run("uses localhost by default", func(t *testing.T) {
		t.Setenv(metricsAddressEnvironmentVariable, "")
		if got := metricsAddressFromEnvironment(); got != defaultMetricsAddress {
			t.Errorf("metricsAddressFromEnvironment() = %q, want %q", got, defaultMetricsAddress)
		}
	})

	t.Run("uses configured address", func(t *testing.T) {
		const address = "0.0.0.0:9091"
		t.Setenv(metricsAddressEnvironmentVariable, address)
		if got := metricsAddressFromEnvironment(); got != address {
			t.Errorf("metricsAddressFromEnvironment() = %q, want %q", got, address)
		}
	})
}
