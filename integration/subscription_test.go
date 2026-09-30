// Package integration runs the generated client against a fake graphql-ws
// server, exercising the real subscription paths: clean completion, transport
// drops and per-subscription GraphQL errors.
//
// The testclient package is generated from pkg/clientgen/testdata/determinism
// by `task test-integration` (or the CI job); it is not committed.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	gographqlclient "github.com/hasura/go-graphql-client"

	"github.com/entwico/gqlgoclientgen/integration/testclient"
)

// gwsMessage is a graphql-ws protocol frame.
type gwsMessage struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// fakeGraphQLWSServer is a minimal graphql-ws (graphql-transport-ws) server.
// The onSubscribe callback decides what to send for each incoming subscribe
// message; it runs in the connection's read loop goroutine.
type fakeGraphQLWSServer struct {
	t           *testing.T
	server      *httptest.Server
	onSubscribe func(conn *websocket.Conn, id string, query string)
}

func newFakeGraphQLWSServer(t *testing.T, onSubscribe func(conn *websocket.Conn, id string, query string)) *fakeGraphQLWSServer {
	t.Helper()

	s := &fakeGraphQLWSServer{t: t, onSubscribe: onSubscribe}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{"graphql-transport-ws"},
		})
		if err != nil {
			return
		}

		ctx := r.Context()
		for {
			var msg gwsMessage
			if err := wsjson.Read(ctx, conn, &msg); err != nil {
				return
			}

			switch msg.Type {
			case "connection_init":
				_ = wsjson.Write(ctx, conn, gwsMessage{Type: "connection_ack"})
			case "ping":
				_ = wsjson.Write(ctx, conn, gwsMessage{Type: "pong"})
			case "subscribe":
				var payload struct {
					Query string `json:"query"`
				}
				_ = json.Unmarshal(msg.Payload, &payload)
				s.onSubscribe(conn, msg.ID, payload.Query)
			case "complete":
				// client-initiated unsubscribe; nothing to do
			}
		}
	}))
	t.Cleanup(s.server.Close)

	return s
}

func (s *fakeGraphQLWSServer) wsURL() string {
	return "ws" + strings.TrimPrefix(s.server.URL, "http")
}

func sendNext(t *testing.T, conn *websocket.Conn, id string, data string) {
	t.Helper()

	err := wsjson.Write(context.Background(), conn, gwsMessage{ID: id, Type: "next", Payload: json.RawMessage(`{"data":` + data + `}`)})
	if err != nil {
		t.Logf("sendNext failed: %v", err)
	}
}

func sendComplete(t *testing.T, conn *websocket.Conn, id string) {
	t.Helper()

	err := wsjson.Write(context.Background(), conn, gwsMessage{ID: id, Type: "complete"})
	if err != nil {
		t.Logf("sendComplete failed: %v", err)
	}
}

func sendError(t *testing.T, conn *websocket.Conn, id string, message string) {
	t.Helper()

	err := wsjson.Write(context.Background(), conn, gwsMessage{ID: id, Type: "error", Payload: json.RawMessage(`[{"message":"` + message + `"}]`)})
	if err != nil {
		t.Logf("sendError failed: %v", err)
	}
}

// startClient connects a generated client to the fake server and returns it
// together with a recorder for lifecycle callbacks.
func startClient(t *testing.T, wsURL string) (*testclient.Client, *callbackRecorder) {
	t.Helper()

	rec := &callbackRecorder{}
	subscriptionClient := gographqlclient.NewSubscriptionClient(wsURL).
		WithProtocol(gographqlclient.GraphQLWS)

	client := testclient.NewClient(testclient.WithGraphQLSubscriptionClient(subscriptionClient))

	err := client.StartSubscriptionClient(
		context.Background(),
		testclient.WithTimeout(5*time.Second),
		testclient.OnSubscriptionError(func(_, operationName string, _ error) {
			rec.record("error:" + operationName)
		}),
		testclient.OnSubscriptionComplete(func(_, operationName string) {
			rec.record("complete:" + operationName)
		}),
	)
	if err != nil {
		t.Fatalf("failed to start subscription client: %v", err)
	}

	return client, rec
}

type callbackRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *callbackRecorder) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *callbackRecorder) has(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e == event {
			return true
		}
	}

	return false
}

const postData = `{"postUpdated":{"id":"1","title":"t","status":"PUBLISHED"}}`

// TestSubscription_CompleteEndsStreamWithoutError: a server-sent GraphQL
// "complete" ends the iterator cleanly after delivering all items.
func TestSubscription_CompleteEndsStreamWithoutError(t *testing.T) {
	server := newFakeGraphQLWSServer(t, func(conn *websocket.Conn, id, _ string) {
		sendNext(t, conn, id, postData)
		sendNext(t, conn, id, postData)
		sendComplete(t, conn, id)
	})

	client, rec := startClient(t, server.wsURL())

	stream, err := client.Subscriptions.WatchPost(context.Background(), "1")
	if err != nil {
		t.Fatalf("failed to start subscription: %v", err)
	}

	var items int
	for resp, err := range stream {
		if err != nil {
			t.Fatalf("iterator yielded unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("iterator yielded nil response without error")
		}
		items++
	}

	if items != 2 {
		t.Fatalf("expected 2 items, got %d", items)
	}
	if !rec.has("complete:watchPost") {
		t.Fatal("onSubscriptionComplete callback was not fired")
	}
	if rec.has("error:watchPost") {
		t.Fatal("onSubscriptionError callback fired on a clean completion")
	}
}

// TestSubscription_TransportErrorEndsStreamWithError: an abnormal socket close
// must surface as a terminal iterator error wrapping ErrSubscriptionTransport —
// never as a clean completion.
func TestSubscription_TransportErrorEndsStreamWithError(t *testing.T) {
	server := newFakeGraphQLWSServer(t, func(conn *websocket.Conn, id, _ string) {
		sendNext(t, conn, id, postData)
		// StatusInternalError (1011) reaches the client's OnError callback
		// (not the library's silent-restart path)
		_ = conn.Close(websocket.StatusInternalError, "boom")
	})

	client, rec := startClient(t, server.wsURL())

	stream, err := client.Subscriptions.WatchPost(context.Background(), "1")
	if err != nil {
		t.Fatalf("failed to start subscription: %v", err)
	}

	var items int
	var streamErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		for resp, err := range stream {
			if err != nil {
				streamErr = err

				return
			}
			if resp != nil {
				items++
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("iterator did not terminate: possible deadlock")
	}

	if items != 1 {
		t.Fatalf("expected 1 item before the drop, got %d", items)
	}
	if streamErr == nil {
		t.Fatal("iterator ended without error after a transport drop: partial stream was reported as complete")
	}
	if !errors.Is(streamErr, testclient.ErrSubscriptionTransport) {
		t.Fatalf("expected error wrapping ErrSubscriptionTransport, got: %v", streamErr)
	}
	if rec.has("complete:watchPost") {
		t.Fatal("onSubscriptionComplete callback fired on a transport drop")
	}
	if !rec.has("error:watchPost") {
		t.Fatal("onSubscriptionError callback was not fired on a transport drop")
	}
}

// TestSubscription_GraphQLErrorIsIsolated: a graphql-ws "error" message for one
// subscription terminates only that subscription's iterator; a second
// subscription on the same connection keeps streaming to completion.
func TestSubscription_GraphQLErrorIsIsolated(t *testing.T) {
	var (
		mu      sync.Mutex
		watchID string
		wconn   *websocket.Conn
	)

	server := newFakeGraphQLWSServer(t, func(conn *websocket.Conn, id, query string) {
		if strings.Contains(query, "postUpdated") {
			mu.Lock()
			watchID, wconn = id, conn
			mu.Unlock()

			return
		}
		// fail the searchUpdated subscription with a GraphQL error, then keep
		// serving the watch subscription on the same connection
		sendError(t, conn, id, "access denied")
		mu.Lock()
		wid, wc := watchID, wconn
		mu.Unlock()
		if wid != "" {
			sendNext(t, wc, wid, postData)
			sendComplete(t, wc, wid)
		}
	})

	client, _ := startClient(t, server.wsURL())

	watchStream, err := client.Subscriptions.WatchPost(context.Background(), "1")
	if err != nil {
		t.Fatalf("failed to start watch subscription: %v", err)
	}

	searchStream, err := client.Subscriptions.WatchSearch(context.Background(), "x")
	if err != nil {
		t.Fatalf("failed to start search subscription: %v", err)
	}

	var searchErr error
	searchDone := make(chan struct{})
	go func() {
		defer close(searchDone)
		for _, err := range searchStream {
			if err != nil {
				searchErr = err

				return
			}
		}
	}()

	var watchItems int
	var watchErr error
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		for resp, err := range watchStream {
			if err != nil {
				watchErr = err

				return
			}
			if resp != nil {
				watchItems++
			}
		}
	}()

	for _, ch := range []chan struct{}{searchDone, watchDone} {
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatal("iterator did not terminate: possible deadlock or cross-subscription teardown")
		}
	}

	if searchErr == nil {
		t.Fatal("search iterator ended without the GraphQL error")
	}
	if !strings.Contains(searchErr.Error(), "access denied") {
		t.Fatalf("search iterator error does not carry the GraphQL error: %v", searchErr)
	}
	if watchErr != nil {
		t.Fatalf("watch subscription was affected by the other subscription's error: %v", watchErr)
	}
	if watchItems != 1 {
		t.Fatalf("expected 1 watch item, got %d", watchItems)
	}
}
