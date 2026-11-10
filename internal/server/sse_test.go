package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBrokerSend(t *testing.T) {
	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	// Register a client
	ch := make(chan SSEEvent, 32)
	broker.register <- ch

	// Give the broker time to register
	time.Sleep(10 * time.Millisecond)

	broker.Send(SSEEvent{Event: "test", Data: `{"msg":"hello"}`})

	select {
	case evt := <-ch:
		if evt.Event != "test" {
			t.Errorf("event = %q, want %q", evt.Event, "test")
		}
		if evt.Data != `{"msg":"hello"}` {
			t.Errorf("data = %q, want %q", evt.Data, `{"msg":"hello"}`)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for SSE event")
	}

	// Unregister
	broker.unregister <- ch
	time.Sleep(10 * time.Millisecond)
}

func TestBrokerContextCancellation(t *testing.T) {
	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		broker.Run(ctx)
		close(done)
	}()

	ch := make(chan SSEEvent, 32)
	broker.register <- ch
	time.Sleep(10 * time.Millisecond)

	cancel()

	select {
	case <-done:
		// OK - broker exited
	case <-time.After(time.Second):
		t.Fatal("broker did not exit after context cancellation")
	}
}

func TestBrokerBroadcastToMultipleClients(t *testing.T) {
	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	ch1 := make(chan SSEEvent, 32)
	ch2 := make(chan SSEEvent, 32)
	broker.register <- ch1
	broker.register <- ch2
	time.Sleep(10 * time.Millisecond)

	broker.Send(SSEEvent{Event: "update", Data: "data"})

	for _, ch := range []chan SSEEvent{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Event != "update" {
				t.Errorf("event = %q, want %q", evt.Event, "update")
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	}
}

func TestServeHTTPSendsHeadersBeforeFirstEvent(t *testing.T) {
	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	srv := httptest.NewServer(broker)
	defer srv.Close()

	// No event is ever sent: the response headers alone must arrive promptly.
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("headers did not arrive before any event: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

// Once the broker stops, open streams end and their handlers return, so the
// HTTP server can shut down; a stream opened after that ends at once.
func TestServeHTTPReturnsWhenBrokerStops(t *testing.T) {
	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	go broker.Run(ctx)
	srv := httptest.NewServer(broker)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	go func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		close(ended)
	}()
	cancel()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("stream still open after the broker stopped")
	}

	late, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	lateDone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, late.Body)
		late.Body.Close()
		close(lateDone)
	}()
	select {
	case <-lateDone:
	case <-time.After(2 * time.Second):
		t.Fatal("a stream opened after the broker stopped stays open")
	}

	closed := make(chan struct{})
	go func() { srv.Close(); close(closed) }() // waits for every handler to return
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("handlers still running after the broker stopped")
	}
}
