package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	gographqlclient "github.com/hasura/go-graphql-client"

	"github.com/entwico/gqlgoclientgen/integration/testclient"
)

const (
	// watchPostID marks a subscription the server keeps open, like a change feed.
	watchPostID = "watch"
	// loadPostID marks a subscription the server answers and completes, like a list load.
	loadPostID = "load"

	reconnectWait = 20 * time.Second
)

// restartableServer is a graphql-ws server whose connections can be dropped with a close
// code and which can refuse connections for a while, the way a restarting backend does.
type restartableServer struct {
	server *httptest.Server
	down   atomic.Bool

	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}
}

func newRestartableServer(t *testing.T) *restartableServer {
	t.Helper()

	s := &restartableServer{conns: make(map[*websocket.Conn]struct{})}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)

	return s
}

func (s *restartableServer) serve(w http.ResponseWriter, r *http.Request) {
	if s.down.Load() {
		http.Error(w, "restarting", http.StatusServiceUnavailable)

		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"graphql-transport-ws"}})
	if err != nil {
		return
	}

	s.mu.Lock()
	s.conns[conn] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()

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
			// a load is answered and completed, anything else stays open
			if strings.Contains(string(msg.Payload), `"`+loadPostID+`"`) {
				_ = wsjson.Write(ctx, conn, gwsMessage{ID: msg.ID, Type: "next", Payload: json.RawMessage(`{"data":` + postData + `}`)})
				_ = wsjson.Write(ctx, conn, gwsMessage{ID: msg.ID, Type: "complete"})
			}
		default:
			// client-initiated complete and pong need no answer
		}
	}
}

func (s *restartableServer) wsURL() string {
	return "ws" + strings.TrimPrefix(s.server.URL, "http")
}

// dropConnections closes every open connection with the given close code.
func (s *restartableServer) dropConnections(code websocket.StatusCode, reason string) {
	s.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(s.conns))

	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close(code, reason)
	}
}

// restart drops every connection the way a backend shutting down does and refuses new
// ones for the given time.
func (s *restartableServer) restart(downtime time.Duration) {
	s.down.Store(true)
	s.dropConnections(websocket.StatusGoingAway, "server is closing the websocket connection")
	time.Sleep(downtime)
	s.down.Store(false)
}

// connectionCount is the number of websocket connections the server holds open.
func (s *restartableServer) connectionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.conns)
}

// requireSingleConnection fails when the client keeps more than one connection: a second
// one belongs to a Run loop that was started without the first one ending.
func requireSingleConnection(t *testing.T, server *restartableServer) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if server.connectionCount() == 1 {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("server holds %d connections of one client, want 1", server.connectionCount())
}

// lifecycle counts the connection callbacks of a client.
type lifecycle struct {
	connected    atomic.Int32
	lost         atomic.Int32
	reconnecting atomic.Int32
}

// startReconnectingClient starts a client configured like a long-running service: with a
// backoff, so that the reconnection loop is active.
func startReconnectingClient(t *testing.T, wsURL string, connectTimeout time.Duration) (*testclient.Client, *lifecycle) {
	t.Helper()

	client, events, _ := startReconnectingClientWithLibraryClient(t, wsURL, connectTimeout)

	return client, events
}

// startReconnectingClientWithLibraryClient also returns the library client, for tests that
// take away the callbacks the generated client relies on.
func startReconnectingClientWithLibraryClient(t *testing.T, wsURL string, connectTimeout time.Duration) (*testclient.Client, *lifecycle, *gographqlclient.SubscriptionClient) {
	t.Helper()

	events := &lifecycle{}
	subscriptionClient := gographqlclient.NewSubscriptionClient(wsURL).
		WithProtocol(gographqlclient.GraphQLWS).
		WithRetryTimeout(connectTimeout)

	client := testclient.NewClient(testclient.WithGraphQLSubscriptionClient(subscriptionClient))

	err := client.StartSubscriptionClient(
		context.Background(),
		testclient.WithBackoff(backoff.NewConstantBackOff(100*time.Millisecond)),
		testclient.WithTimeout(connectTimeout),
		testclient.OnConnected(func() { events.connected.Add(1) }),
		testclient.OnConnectionLost(func() { events.lost.Add(1) }),
		testclient.OnReconnecting(func() { events.reconnecting.Add(1) }),
	)
	if err != nil {
		t.Fatalf("failed to start subscription client: %v", err)
	}

	return client, events, subscriptionClient
}

// keepWatching holds a change-feed subscription open and starts it again whenever it
// ends, the way a repository watcher does.
func keepWatching(t *testing.T, client *testclient.Client) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		for ctx.Err() == nil {
			stream, err := client.Subscriptions.WatchPost(ctx, watchPostID)
			if err == nil {
				for range stream { //nolint:revive // drained until the stream ends
				}
			}

			select {
			case <-ctx.Done():
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
}

// loadCompletes runs one load subscription and reports whether it delivered its item
// before the timeout. A client that only believes it is connected never delivers.
func loadCompletes(client *testclient.Client, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	stream, err := client.Subscriptions.WatchPost(ctx, loadPostID)
	if err != nil {
		return false
	}

	items := 0

	for resp, streamErr := range stream {
		if streamErr != nil {
			return false
		}

		if resp != nil {
			items++
		}
	}

	return items == 1
}

// requireLoadsAgain waits until a load goes through, which takes a live connection.
func requireLoadsAgain(t *testing.T, client *testclient.Client, after string) {
	t.Helper()

	deadline := time.Now().Add(reconnectWait)
	for time.Now().Before(deadline) {
		if loadCompletes(client, 2*time.Second) {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("no load completed within %s after %s: the client did not reconnect", reconnectWait, after)
}

func requireCount(t *testing.T, counter *atomic.Int32, atLeast int32, what string) {
	t.Helper()

	deadline := time.Now().Add(reconnectWait)
	for time.Now().Before(deadline) {
		if counter.Load() >= atLeast {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("%s: got %d, want at least %d", what, counter.Load(), atLeast)
}

// TestReconnect_SurvivesRepeatedServerRestarts: every restart of the server is followed by
// a working connection, not only the first one.
func TestReconnect_SurvivesRepeatedServerRestarts(t *testing.T) {
	t.Parallel()

	server := newRestartableServer(t)
	client, events := startReconnectingClient(t, server.wsURL(), 5*time.Second)

	keepWatching(t, client)
	requireLoadsAgain(t, client, "the initial connect")

	for restart := int32(1); restart <= 3; restart++ {
		server.restart(300 * time.Millisecond)

		requireCount(t, &events.lost, restart, "connection lost callbacks")
		requireCount(t, &events.connected, restart+1, "connected callbacks")
		requireLoadsAgain(t, client, "a server restart")
	}

	requireSingleConnection(t, server)
}

// TestReconnect_SurvivesLibraryRestartFollowedByServerRestart: a close code the library
// recovers from on its own must not leave the client unable to notice the next drop.
func TestReconnect_SurvivesLibraryRestartFollowedByServerRestart(t *testing.T) {
	t.Parallel()

	server := newRestartableServer(t)
	client, _ := startReconnectingClient(t, server.wsURL(), 5*time.Second)

	keepWatching(t, client)
	requireLoadsAgain(t, client, "the initial connect")

	server.dropConnections(websocket.StatusServiceRestart, "service restart")
	requireLoadsAgain(t, client, "a service restart close")

	server.restart(300 * time.Millisecond)
	requireLoadsAgain(t, client, "a server restart after a service restart close")

	server.restart(300 * time.Millisecond)
	requireLoadsAgain(t, client, "a second server restart")
	requireSingleConnection(t, server)
}

// TestReconnect_SurvivesRestartLongerThanConnectTimeout: a server that stays away longer
// than one connect attempt lasts is still reconnected to, and so is the restart after it.
func TestReconnect_SurvivesRestartLongerThanConnectTimeout(t *testing.T) {
	t.Parallel()

	server := newRestartableServer(t)
	client, _ := startReconnectingClient(t, server.wsURL(), time.Second)

	keepWatching(t, client)
	requireLoadsAgain(t, client, "the initial connect")

	server.restart(4 * time.Second)
	requireLoadsAgain(t, client, "a long server restart")

	server.restart(300 * time.Millisecond)
	requireLoadsAgain(t, client, "a server restart after a long one")
	requireSingleConnection(t, server)
}

// TestReconnect_SubscribeFailsWhileDisconnected: a subscription started while the server
// is away is refused or ends, it is never accepted and then left without an answer.
func TestReconnect_SubscribeFailsWhileDisconnected(t *testing.T) {
	t.Parallel()

	server := newRestartableServer(t)
	client, events := startReconnectingClient(t, server.wsURL(), time.Second)

	requireLoadsAgain(t, client, "the initial connect")

	server.down.Store(true)
	server.dropConnections(websocket.StatusGoingAway, "server is closing the websocket connection")
	requireCount(t, &events.lost, 1, "connection lost callbacks")

	ended := make(chan struct{})

	go func() {
		defer close(ended)

		stream, err := client.Subscriptions.WatchPost(context.Background(), loadPostID)
		if err != nil {
			return
		}

		for range stream { //nolint:revive // drained until the stream ends
		}
	}()

	select {
	case <-ended:
	case <-time.After(reconnectWait):
		t.Fatal("a subscription started while disconnected neither failed nor ended")
	}
}

// TestReconnect_DoesNotDependOnLibraryCallbacks: the library can leave its Run loop without
// reporting a disconnect and without telling the subscriptions. The client must reconnect
// then too, and an open stream must end with an error instead of waiting forever.
func TestReconnect_DoesNotDependOnLibraryCallbacks(t *testing.T) {
	t.Parallel()

	server := newRestartableServer(t)
	client, events, libraryClient := startReconnectingClientWithLibraryClient(t, server.wsURL(), 5*time.Second)

	requireLoadsAgain(t, client, "the initial connect")

	stream, err := client.Subscriptions.WatchPost(context.Background(), watchPostID)
	if err != nil {
		t.Fatalf("failed to start subscription: %v", err)
	}

	streamErr := make(chan error, 1)

	go func() {
		for _, err := range stream {
			if err != nil {
				streamErr <- err

				return
			}
		}

		streamErr <- nil
	}()

	// from here on the library ends its Run loop on an error but reports nothing
	libraryClient.
		OnDisconnected(func() {}).
		OnError(func(_ *gographqlclient.SubscriptionClient, err error) error { return err })

	server.restart(300 * time.Millisecond)

	select {
	case err := <-streamErr:
		if !errors.Is(err, testclient.ErrSubscriptionTransport) {
			t.Fatalf("open stream ended with %v, want an error wrapping ErrSubscriptionTransport", err)
		}
	case <-time.After(reconnectWait):
		t.Fatal("open stream neither failed nor ended after the connection was lost")
	}

	requireCount(t, &events.lost, 1, "connection lost callbacks")
	requireLoadsAgain(t, client, "a drop the library did not report")
	requireSingleConnection(t, server)
}
