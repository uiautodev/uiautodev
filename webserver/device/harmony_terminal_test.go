package device

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHarmonyTerminalHandshakeAndStream(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	helloCh := make(chan []byte, 1)
	shellCh := make(chan []byte, 1)
	inputCh := make(chan []byte, 1)

	go func() {
		if err := writeHCDFrame(server, []byte("OHOS HDC\x00\x00\x00\x00")); err != nil {
			return
		}

		hello, err := readHCDFrame(server)
		if err != nil {
			return
		}
		helloCh <- hello

		shell, err := readHCDFrame(server)
		if err != nil {
			return
		}
		shellCh <- shell

		if err := writeHCDFrame(server, []byte("hello\r\n")); err != nil {
			return
		}
		input, err := readHCDFrame(server)
		if err != nil {
			return
		}
		inputCh <- input

		_ = writeHCDFrame(server, []byte("file1\r\n"))
		_ = server.Close()
	}()

	terminal, err := startHarmonyShell(client, "serial-1")
	require.NoError(t, err)
	defer terminal.Close()

	hello := <-helloCh
	require.Len(t, hello, 12+32)
	require.Equal(t, "OHOS HDC", string(hello[:8]))
	require.Equal(t, "serial-1", string(hello[12:20]))
	require.Equal(t, "shell", string(<-shellCh))

	buffer := make([]byte, 32)
	n, err := terminal.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, "hello\r\n", string(buffer[:n]))

	written, err := terminal.Write([]byte("ls\r"))
	require.NoError(t, err)
	require.Equal(t, 3, written)
	require.Equal(t, "ls\r", string(<-inputCh))

	n, err = terminal.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, "file1\r\n", string(buffer[:n]))

	_, err = terminal.Read(buffer)
	require.ErrorIs(t, err, io.EOF)

	code, err := terminal.Wait()
	require.NoError(t, err)
	require.Zero(t, code)
}

func TestHarmonyTerminalRejectsBadHandshake(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	go func() {
		_ = writeHCDFrame(server, []byte("not hdc"))
	}()

	_, err := startHarmonyShell(client, "serial-1")
	require.ErrorContains(t, err, "unexpected hdc handshake")
}

func TestHarmonyTerminalCloseUnblocksReadAndWait(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	go func() {
		_ = writeHCDFrame(server, []byte("OHOS HDC\x00\x00\x00\x00"))
		_, _ = readHCDFrame(server)
		_, _ = readHCDFrame(server)
	}()

	terminal, err := startHarmonyShell(client, "serial-1")
	require.NoError(t, err)

	readErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 16)
		_, err := terminal.Read(buffer)
		readErr <- err
	}()

	require.NoError(t, terminal.Close())
	select {
	case err := <-readErr:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not unblock after Close")
	}

	waited := make(chan struct{})
	go func() {
		code, err := terminal.Wait()
		if err != nil {
			t.Errorf("Wait: %v", err)
		}
		if code != 0 {
			t.Errorf("Wait code = %d, want 0", code)
		}
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
}

func TestReadHCDFrameRejectsOversizedFrame(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	go func() {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], maxHCDFrameSize+1)
		_, _ = server.Write(header[:])
	}()

	_, err := readHCDFrame(client)
	require.ErrorContains(t, err, "too large")
}
