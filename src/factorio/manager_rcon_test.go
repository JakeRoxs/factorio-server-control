package factorio

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/OpenFactorioServerManager/rcon"
	"github.com/stretchr/testify/require"
)

func TestManagerWatchdogConsumesItsOwnRCONResponse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		commands := 0
		for commands < 2 {
			var length [4]byte
			if _, err := io.ReadFull(connection, length[:]); err != nil {
				done <- err
				return
			}
			size := binary.LittleEndian.Uint32(length[:])
			if size < 10 || size > 4096 {
				done <- errors.New("invalid fixture RCON frame")
				return
			}
			body := make([]byte, size)
			if _, err := io.ReadFull(connection, body); err != nil {
				done <- err
				return
			}
			id, kind := binary.LittleEndian.Uint32(body[:4]), binary.LittleEndian.Uint32(body[4:8])
			response, responseKind := "", uint32(2)
			if kind != 3 {
				commands++
				responseKind = 0
				if commands == 1 {
					response = "0"
				} else {
					response = "saved"
				}
			}
			frame := make([]byte, 14+len(response))
			binary.LittleEndian.PutUint32(frame[:4], uint32(len(frame)-4))
			binary.LittleEndian.PutUint32(frame[4:8], id)
			binary.LittleEndian.PutUint32(frame[8:12], responseKind)
			copy(frame[12:], response)
			if _, err := connection.Write(frame); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	console, err := rcon.Dial(listener.Addr().String(), "synthetic-fixture-password")
	require.NoError(t, err)
	t.Cleanup(func() { _ = console.Close() })
	server := &Server{ID: "2", Running: true, RconConnected: true, Rcon: console, WatchdogInterval: 1}
	manager := &ServerManager{}
	manager.watchdogCheckServer(server)
	response, err := server.RunPersistentRCONCommand("/server-save")
	require.NoError(t, err, "a watchdog response must not be left queued for the next command")
	require.Equal(t, "saved", response)
	require.NoError(t, <-done)
}
