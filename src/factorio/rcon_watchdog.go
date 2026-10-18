package factorio

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
)

var (
	rconWatchdogCancel    chan struct{}
	rconWatchdogOnce      sync.Once
	rconWatchdogMutex     sync.Mutex
	rconWatchdogRunning   bool
	rconWatchdogConnected bool
)

func GetRconWatchdogConnected() bool {
	rconWatchdogMutex.Lock()
	defer rconWatchdogMutex.Unlock()
	return rconWatchdogConnected
}

func setRconWatchdogConnected(connected bool) {
	rconWatchdogMutex.Lock()
	if rconWatchdogConnected == connected {
		rconWatchdogMutex.Unlock()
		return
	}
	rconWatchdogConnected = connected
	rconWatchdogMutex.Unlock()

	if connected {
		log.Printf("RCON watchdog: connection healthy")
	} else {
		log.Printf("RCON watchdog: connection lost")
	}
	GetFactorioServer().broadcastStatus()
}

func startRconWatchdog() {
	rconWatchdogOnce.Do(func() {
		rconWatchdogCancel = make(chan struct{})
	})
	rconWatchdogMutex.Lock()
	if rconWatchdogRunning {
		rconWatchdogMutex.Unlock()
		return
	}
	rconWatchdogRunning = true
	rconWatchdogMutex.Unlock()

	interval := bootstrap.GetConfig().RconWatchdogInterval
	if interval <= 0 {
		interval = 30
	}
	go rconWatchdogLoop(interval)
}

func stopRconWatchdog() {
	rconWatchdogMutex.Lock()
	if !rconWatchdogRunning {
		rconWatchdogMutex.Unlock()
		return
	}
	rconWatchdogRunning = false
	rconWatchdogMutex.Unlock()

	select {
	case <-rconWatchdogCancel:
	default:
		close(rconWatchdogCancel)
	}
	setRconWatchdogConnected(false)
}

func rconWatchdogLoop(interval int) {
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rconWatchdogCancel:
			return
		case <-ticker.C:
			rconWatchdogProbe()
		}
	}
}

func rconWatchdogProbe() {
	server := GetFactorioServer()
	if !server.GetRunning() {
		return
	}

	_, err := server.runPersistentRCONCommand("status")
	if err == nil {
		setRconWatchdogConnected(true)
		return
	}

	log.Printf("RCON watchdog: probe failed: %s", err)
	setRconWatchdogConnected(false)

	if err := reconnectRCON(); err != nil {
		log.Printf("RCON watchdog: reconnect failed: %s", err)
		return
	}

	_, err = server.runPersistentRCONCommand("status")
	if err == nil {
		log.Printf("RCON watchdog: reconnected successfully")
		setRconWatchdogConnected(true)
	} else {
		log.Printf("RCON watchdog: still failing after reconnect: %s", err)
	}
}

func reconnectRCON() error {
	server := GetFactorioServer()
	if !server.GetRunning() {
		return errors.New("server not running")
	}

	server.closeRcon()
	return connectRC()
}