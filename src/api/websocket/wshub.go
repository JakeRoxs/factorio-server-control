package websocket

import (
	"reflect"
	"sync"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
)

// the hub, that is exported and can be used anywhere to work with the websocket
var WebsocketHub *wsHub

var LogCache []string

// a controlHandler is used to determine, if something has to be done, on a specific command.
// register a handler with `wsHub.RegisterControlHandler`
// unregister a handler with `wsHub.UnregisterControlHandler`
type controlHandler func(controls WsControls)

// The type for of control messages.
// Type ans Value both have to be set, if controls are sent.
// Currently supported Type:
// - `subscribe` - Value used as room name
// - `unsubscribe` - Value used as room name
// - `command` - Value contains the command to execute
type WsControls struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// the main message of our websocket protocol.
// if the room_name is an empty string, this will be sent as broadcast
// if controls is not empty, this will not be sent anywhere, but used as commands, to join/leave rooms and to send commands to the factorio server
type wsMessage struct {
	RoomName string      `json:"room_name"`
	Message  interface{} `json:"message,omitempty"`
	Controls WsControls  `json:"controls,omitempty"`
}

// wsRoom is a single room owned by the wsHub for the lifetime of the hub.
//
// Lifetime policy (intentional retention):
//   - Rooms are created on demand by wsHub.GetRoom and are never removed from
//     the hub's rooms map, even when the last client unregisters. A wsRoom
//     pointer is therefore a stable handle: GetRoom returns the same pointer for
//     a given name, and that pointer remains valid after empty unregister and
//     rejoin.
//   - Unregister only removes the client from the room's client map; it does not
//     delete the room.
//   - The hub owns the rooms map (serialized by roomsMu). Room state (the client
//     map) is owned by the room's own run() goroutine, so it is never accessed
//     concurrently.
//   - Deleting empty rooms was rejected: callers in factorio/server.go retain
//     room pointers and call Send on them (broadcastStatus, game log output,
//     RCON responses), and dynamic per-server rooms are created on demand.
//     Deleting a room would require coordinated hub ownership to avoid
//     split-room/race hazards (a client subscribing to a room being deleted, or
//     a caller holding a stale pointer).
type wsRoom struct {
	// same as the key of the map in the wsHub
	name string

	// clients that are in this room. This list is a sublist of the one inside the hub
	clients map[*wsClient]bool

	// register a client to this room
	register chan *wsClient

	// unregister a client from this room. The room itself is never deleted: it
	// remains a stable handle owned by the hub for its lifetime. Unregister only
	// removes the client from the room's client map.
	unregister chan *wsClient

	// send a message to all clients in this room
	send chan wsMessage
}

// Hub is the basic setup of the server.
// It contains everything needed for the websocket to run.
// Only the controlHandler Subscriptions are public, everything else can be controlled with the functions and the wsClient.
type wsHub struct {
	// list of all connected clients
	clients map[*wsClient]bool

	// Messages that should be sent to ALL clients
	broadcast chan wsMessage

	// a list of all rooms
	rooms map[string]*wsRoom
	// rooms is also accessed by HTTP/WebSocket client goroutines through
	// GetRoom, independently from the hub event loop.
	roomsMu sync.RWMutex

	// register a client to this hub
	register chan *wsClient

	// unregister a client from this hub
	unregister chan *wsClient

	// run a control message on all registered controlHandler
	runControl chan WsControls

	// list of all registered controlHandlers
	controlHandlers map[reflect.Value]controlHandler
	controlMu       sync.RWMutex

	// register a controlHandler
	RegisterControlHandler chan controlHandler

	// unregister a controlHandler
	UnregisterControlHandler chan controlHandler
}

// initialize and run the mein websocket hub.
func init() {
	WebsocketHub = &wsHub{
		broadcast:                make(chan wsMessage),
		register:                 make(chan *wsClient),
		rooms:                    make(map[string]*wsRoom),
		unregister:               make(chan *wsClient),
		clients:                  make(map[*wsClient]bool),
		runControl:               make(chan WsControls),
		controlHandlers:          make(map[reflect.Value]controlHandler),
		RegisterControlHandler:   make(chan controlHandler),
		UnregisterControlHandler: make(chan controlHandler),
	}

	go WebsocketHub.run()
}

// remove a client from this hub and all of its rooms
func (hub *wsHub) removeClient(client *wsClient) {
	delete(hub.clients, client)
	// Do not close send while independent room goroutines may still be
	// selecting a send to it. Closing the socket makes the pumps exit, and the
	// client becomes collectible once it has been removed from every room.
	if client.conn != nil {
		_ = client.conn.Close()
	}
	hub.roomsMu.RLock()
	rooms := make([]*wsRoom, 0, len(hub.rooms))
	for _, room := range hub.rooms {
		rooms = append(rooms, room)
	}
	hub.roomsMu.RUnlock()
	for _, room := range rooms {
		room.unregister <- client
	}
}

// run starts a websocket hub, this has to be done in a subroutine `go hub.run()`
func (hub *wsHub) run() {
	for {
		select {
		case client := <-hub.register:
			hub.clients[client] = true
		case client := <-hub.unregister:
			if _, ok := hub.clients[client]; ok {
				hub.removeClient(client)
			}
		case message := <-hub.broadcast:
			for client := range hub.clients {
				select {
				case client.send <- message:
				default:
					hub.removeClient(client)
				}
			}
		case function := <-hub.RegisterControlHandler:
			hub.controlMu.Lock()
			hub.controlHandlers[reflect.ValueOf(function)] = function
			hub.controlMu.Unlock()
		case function := <-hub.UnregisterControlHandler:
			hub.controlMu.Lock()
			delete(hub.controlHandlers, reflect.ValueOf(function))
			hub.controlMu.Unlock()
		}
	}
}

func (hub *wsHub) dispatchControl(controls WsControls) {
	hub.controlMu.RLock()
	handlers := make([]controlHandler, 0, len(hub.controlHandlers))
	for _, handler := range hub.controlHandlers {
		handlers = append(handlers, handler)
	}
	hub.controlMu.RUnlock()
	for _, handler := range handlers {
		go handler(controls)
	}
}

// Broadcast a message to all connected clients (only clients connected to this room).
func (hub *wsHub) Broadcast(message interface{}) {
	hub.broadcast <- wsMessage{
		RoomName: "",
		Message:  message,
	}
}

// get a websocket room or create it, if it doesn't exist yet.
// Also starts the rooms subroutine `wsRoom.run()`
func (hub *wsHub) GetRoom(name string) *wsRoom {
	hub.roomsMu.RLock()
	if room, ok := hub.rooms[name]; ok {
		hub.roomsMu.RUnlock()
		return room
	}
	hub.roomsMu.RUnlock()

	hub.roomsMu.Lock()
	defer hub.roomsMu.Unlock()
	if room, ok := hub.rooms[name]; ok {
		return room
	}
	room := &wsRoom{
		name:       name,
		clients:    make(map[*wsClient]bool),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		send:       make(chan wsMessage),
	}
	hub.rooms[name] = room
	go room.run()
	return room
}

// run starts a websocket room. This has to be run as a subroutine `go room.run()`
func (room *wsRoom) run() {
	for {
		select {
		case client := <-room.register:
			room.handleRegister(client)
		case client := <-room.unregister:
			room.handleUnregister(client)
		case message := <-room.send:
			room.handleSend(message)
		}
	}
}

// handleRegister subscribes a client to the room when the client is allowed,
// then replays the cached game log for the gamelog room. A client whose send
// channel cannot keep up is dropped during replay. The room itself is never
// deleted; see the room lifetime policy above.
func (room *wsRoom) handleRegister(client *wsClient) {
	if !client.maySubscribeRoom(room.name) {
		return
	}
	room.clients[client] = true
	if room.name == "gamelog" {
		room.replayLogCache(client)
	}
}

// replayLogCache sends every cached log line to the client in order. If the
// client's send channel is full, the client is dropped from the room and replay
// stops. This preserves the existing bounded, non-blocking replay.
func (room *wsRoom) replayLogCache(client *wsClient) {
	for _, logLine := range LogCache {
		select {
		case client.send <- wsMessage{
			RoomName: room.name,
			Message:  logLine,
		}:
		default:
			delete(room.clients, client)
			return
		}
	}
}

// handleUnregister removes a client from the room. Rooms are intentionally
// never deleted from the hub, even when empty; see the room lifetime policy.
func (room *wsRoom) handleUnregister(client *wsClient) {
	if _, ok := room.clients[client]; ok {
		delete(room.clients, client)
	}
}

// handleSend broadcasts a message to every client in the room. Clients that are
// no longer authorized or whose send channel is full are dropped. For the
// gamelog room, the message is also appended to the shared log cache.
func (room *wsRoom) handleSend(message wsMessage) {
	for client := range room.clients {
		if !client.maySubscribeRoom(room.name) {
			delete(room.clients, client)
			continue
		}
		select {
		case client.send <- message:
		default:
			delete(room.clients, client)
		}
	}
	if room.name == "gamelog" {
		room.appendLogCache(message)
	}
}

// appendLogCache appends a gamelog line to the shared cache and trims it to
// the configured maximum size, preserving insertion order.
func (room *wsRoom) appendLogCache(message wsMessage) {
	LogCache = append(LogCache, message.Message.(string))
	config := bootstrap.GetConfig()
	if len(LogCache) > config.ConsoleCacheSize {
		LogCache = LogCache[1:]
	}
}

// Send a message into this room.
func (room *wsRoom) Send(message interface{}) {
	room.send <- wsMessage{
		RoomName: room.name,
		Message:  message,
	}
}
