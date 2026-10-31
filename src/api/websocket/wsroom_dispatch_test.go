package websocket

import (
	"testing"
)

func TestGetRoomReturnsStablePointerForSameName(t *testing.T) {
	hub := &wsHub{rooms: make(map[string]*wsRoom)}

	room1 := hub.GetRoom("server_status")
	room2 := hub.GetRoom("server_status")
	if room1 != room2 {
		t.Fatal("GetRoom must return the same stable pointer for the same room name")
	}
}

func TestRoomStaysInHubAfterAllClientsUnregister(t *testing.T) {
	hub := &wsHub{rooms: make(map[string]*wsRoom)}
	room := hub.GetRoom("server_status")

	client := &wsClient{send: make(chan wsMessage)}
	room.handleRegister(client)
	if !room.clients[client] {
		t.Fatal("client should be registered to the room")
	}

	room.handleUnregister(client)
	if room.clients[client] {
		t.Fatal("client should be removed from the room after unregister")
	}

	// The room must remain in the hub and return the same pointer.
	again := hub.GetRoom("server_status")
	if again != room {
		t.Fatal("GetRoom must return the same stable pointer after the room is empty")
	}
}

func TestRoomAllowsRejoinAfterEmptyUnregister(t *testing.T) {
	hub := &wsHub{rooms: make(map[string]*wsRoom)}
	room := hub.GetRoom("server_status")

	client := &wsClient{send: make(chan wsMessage)}
	room.handleRegister(client)
	room.handleUnregister(client)

	// Rejoin the client to the same (never-deleted) room.
	room.handleRegister(client)
	if !room.clients[client] {
		t.Fatal("client should be able to rejoin an existing room")
	}
}

func TestHandleRegisterRejectsUnauthorizedClientForGamelog(t *testing.T) {
	room := &wsRoom{
		name:       "gamelog",
		clients:    make(map[*wsClient]bool),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		send:       make(chan wsMessage),
	}

	client := &wsClient{
		send:          make(chan wsMessage),
		allowCommands: func() bool { return false },
	}

	// Unauthorized clients are rejected at subscribe time; no cache replay
	// happens, so the shared log cache is not touched.
	room.handleRegister(client)

	if room.clients[client] {
		t.Fatal("unauthorized client should not be registered to the gamelog room")
	}
}

func TestHandleSendDropsClientWithFullSendChannel(t *testing.T) {
	room := &wsRoom{
		name:       "server_status",
		clients:    make(map[*wsClient]bool),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		send:       make(chan wsMessage),
	}

	client := &wsClient{send: make(chan wsMessage, 0)}
	room.clients[client] = true

	// Non-blocking send: a full client channel drops the client instead of
	// blocking the room's dispatch goroutine.
	room.handleSend(wsMessage{RoomName: room.name, Message: "test"})

	if room.clients[client] {
		t.Fatal("client with a full send channel should be dropped")
	}
}
