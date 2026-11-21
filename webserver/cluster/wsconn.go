package cluster

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsConn adapts a WebSocket connection to net.Conn so it can carry a yamux
// session. Each WebSocket binary message is treated as part of a byte stream.
type wsConn struct {
	conn *websocket.Conn

	readMu  sync.Mutex
	reader  io.Reader
	writeMu sync.Mutex
	closeMu sync.Once
}

func newWSConn(conn *websocket.Conn) net.Conn {
	return &wsConn{conn: conn}
}

func (c *wsConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for {
		if c.reader == nil {
			messageType, reader, err := c.conn.NextReader()
			if err != nil {
				return 0, err
			}
			if messageType != websocket.BinaryMessage {
				continue
			}
			c.reader = reader
		}
		n, err := c.reader.Read(p)
		if err == io.EOF {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *wsConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	writer, err := c.conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	n, err := writer.Write(p)
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	return n, err
}

func (c *wsConn) Close() error {
	var err error
	c.closeMu.Do(func() {
		_ = c.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		err = c.conn.Close()
	})
	return err
}

func (c *wsConn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *wsConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

func (c *wsConn) SetDeadline(t time.Time) error {
	if err := c.conn.SetReadDeadline(t); err != nil {
		return err
	}
	return c.conn.SetWriteDeadline(t)
}

func (c *wsConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *wsConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
