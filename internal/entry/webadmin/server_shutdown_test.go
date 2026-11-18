package webadmin

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHTTPShutdownWaitsForActiveResponse(t *testing.T) {
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := reservation.Addr().String()
	_ = reservation.Close()
	entered, release, stopping := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := &http.Server{Addr: addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "restart acknowledged")
	})}
	server.RegisterOnShutdown(func() { close(stopping) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- (&Server{}).serveHTTP(ctx, server) }()
	t.Cleanup(func() { _ = server.Close() })
	response := make(chan string, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		deadline := time.Now().Add(2 * time.Second)
		for {
			resp, err := client.Get("http://" + addr)
			if err != nil {
				if time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
					continue
				}
				response <- err.Error()
				return
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				response <- err.Error()
				return
			}
			response <- string(body)
			return
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach the HTTP handler")
	}
	cancel()
	select {
	case <-stopping:
	case <-time.After(time.Second):
		t.Fatal("HTTP shutdown did not begin")
	}
	var returnedEarly bool
	select {
	case <-done:
		returnedEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if returnedEarly {
		t.Fatal("Web child returned while the active response was still blocked")
	}
	select {
	case body := <-response:
		if body != "restart acknowledged" {
			t.Fatalf("response = %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("active response was lost during shutdown")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP shutdown did not finish after the response")
	}
}
