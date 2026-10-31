package websocket

import (
	"testing"
)

func TestHandleIncomingMessageIgnoresNonControlMessages(t *testing.T) {
	client := &wsClient{allowCommands: func() bool { return true }}

	// Non-empty room name: ignored regardless of controls
	client.handleIncomingMessage(wsMessage{RoomName: "gamelog", Controls: WsControls{Type: "subscribe", Value: "gamelog"}})

	// Empty room name, empty controls: ignored
	client.handleIncomingMessage(wsMessage{RoomName: "", Controls: WsControls{}})

	// Non-control type: ignored
	client.handleIncomingMessage(wsMessage{RoomName: "", Controls: WsControls{Type: "unknown", Value: "x"}})
}

func TestMaySubscribeRoomRespectsAllowlistAndCurrentAuthorization(t *testing.T) {
	admin := false
	client := &wsClient{allowCommands: func() bool { return admin }}

	// Not allowlisted: rejected
	if client.maySubscribeRoom("arbitrary") {
		t.Fatal("non-allowlisted room was allowed")
	}

	// Allowlisted but not admin: gamelog rejected
	if client.maySubscribeRoom("gamelog") {
		t.Fatal("viewer was allowed to subscribe to gamelog")
	}

	// server_status is read-only: allowed without admin
	if !client.maySubscribeRoom("server_status") {
		t.Fatal("viewer could not subscribe to read-only server status")
	}

	// Admin: gamelog allowed
	admin = true
	if !client.maySubscribeRoom("gamelog") {
		t.Fatal("admin client could not subscribe to gamelog")
	}

	// Revoked: rejected again
	admin = false
	if client.maySubscribeRoom("gamelog") {
		t.Fatal("revoked client retained gamelog subscription")
	}
}

func TestHandleUnsubscribeRespectsAllowlist(t *testing.T) {
	hub := &wsHub{rooms: make(map[string]*wsRoom)}
	client := &wsClient{
		hub:           hub,
		allowCommands: func() bool { return true },
	}

	client.handleUnsubscribe("gamelog")
	client.handleUnsubscribe("server_status")

	// Non-allowlisted rooms are ignored
	client.handleUnsubscribe("arbitrary")
	client.handleUnsubscribe("")
	client.handleUnsubscribe("../gamelog")
}

func TestMayDispatchControlRequiresCurrentAdminCapability(t *testing.T) {
	command := WsControls{Type: "command", Value: "/players online"}
	admin := false
	client := &wsClient{allowCommands: func() bool { return admin }}

	if client.mayDispatchControl(command) {
		t.Fatal("read-only client was allowed to dispatch a command")
	}

	admin = true
	if !client.mayDispatchControl(command) {
		t.Fatal("admin client could not dispatch a command")
	}

	admin = false
	if client.mayDispatchControl(command) {
		t.Fatal("revoked client retained command access")
	}
}

func TestHandleControlMessageUnknownTypeIgnored(t *testing.T) {
	client := &wsClient{allowCommands: func() bool { return true }}
	client.handleControlMessage(WsControls{Type: "unknown", Value: "x"})
}
