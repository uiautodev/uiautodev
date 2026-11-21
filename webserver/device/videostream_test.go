package device

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestStartWebsocketReaderDispatchesMessages(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		ctx, cancel := startWebsocketReader(r.Context(), conn, func(_ context.Context, data []byte) {
			select {
			case received <- string(data):
			default:
			}
		})
		defer cancel()
		<-ctx.Done()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"touch"}`)))

	select {
	case got := <-received:
		require.Equal(t, `{"type":"touch"}`, got)
	case <-time.After(2 * time.Second):
		t.Fatal("control message was not dispatched")
	}
}

func TestCancelOnWebsocketClose(t *testing.T) {
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		ctx, cancel := cancelOnWebsocketClose(r.Context(), conn)
		defer cancel()

		<-ctx.Done()
		close(serverDone)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.NoError(t, err)

	// Closing the client must cancel the server-side context.
	require.NoError(t, conn.Close())

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server context was not canceled after client close")
	}
}
