package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	h := http.NewServeMux()
	srv := newServer(h)
	if srv.Handler != h {
		t.Fatalf("newServer(h).Handler = %v, want h", srv.Handler)
	}
	if srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("ReadTimeout = %v, WriteTimeout = %v, IdleTimeout = %v; set all three, or a slow client can hold a connection open forever",
			srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
}

// TestRunShutsDownGracefully stands in for Ctrl-C: it cancels the context run
// was given while a request is still being handled. run must stop accepting
// new connections, let that request finish, and only then return nil.
func TestRunShutsDownGracefully(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, finish := make(chan struct{}), make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-finish
		io.WriteString(w, "done")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- run(ctx, newServer(slow), ln) }()

	type reply struct {
		body string
		err  error
	}
	replied := make(chan reply, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			replied <- reply{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		replied <- reply{string(body), err}
	}()
	select {
	case <-started:
	case err := <-ran:
		t.Fatalf("run returned %v before it was cancelled; it should keep serving", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the handler; does run serve on the listener it is given?")
	}

	cancel()
	// Shutdown closes the listener first, so new connections are refused soon
	// after the cancel.
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			break
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("still accepting connections 5s after the context was cancelled; call srv.Shutdown when ctx is done")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-ran:
		t.Fatalf("run returned %v while a request was still in flight; Shutdown waits for it, Close does not", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(finish)
	if r := <-replied; r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request got %q, %v; want it answered in full", r.body, r.err)
	}
	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("run returned %v after a clean shutdown, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return 5s after the last request finished")
	}
}

// TestRunReturnsServeError checks that when Serve fails, run says so at once
// instead of waiting for a signal that may never come.
func TestRunReturnsServeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	ran := make(chan error, 1)
	go func() { ran <- run(context.Background(), newServer(http.NewServeMux()), ln) }()
	select {
	case err := <-ran:
		if err == nil {
			t.Fatal("run returned nil although Serve failed; return Serve's error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return 5s after Serve failed; also wait on Serve's error, not only on ctx")
	}
}
