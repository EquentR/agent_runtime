package commands

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeHTTPUntilCanceledStopsAcceptingAndReturns(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveHTTPUntilCanceled(ctx, listener, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}), time.Second)
	}()

	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatalf("GET before shutdown: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveHTTPUntilCanceled() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serveHTTPUntilCanceled did not return")
	}
}
