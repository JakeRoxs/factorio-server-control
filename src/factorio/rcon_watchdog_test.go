package factorio

import (
	"sync"
	"testing"
	"time"
)

func TestRconWatchdogConnectedState(t *testing.T) {
	if GetRconWatchdogConnected() {
		t.Error("expected initial state to be false")
	}
}

func TestRconWatchdogStartStop(t *testing.T) {
	startRconWatchdog()
	defer stopRconWatchdog()
}

func TestRconWatchdogStartIdempotent(t *testing.T) {
	startRconWatchdog()
	startRconWatchdog()
	startRconWatchdog()
	stopRconWatchdog()
}

func TestRconWatchdogStopIdempotent(t *testing.T) {
	stopRconWatchdog()
	stopRconWatchdog()
}

func TestRconWatchdogProbeWithNoServer(t *testing.T) {
	rconWatchdogProbe()
}

func TestRconWatchdogReconnectWithNoServer(t *testing.T) {
	err := reconnectRCON()
	if err == nil {
		t.Error("expected reconnect to fail when server is not running")
	}
}

func TestRconWatchdogLoopCancellation(t *testing.T) {
	startRconWatchdog()
	time.Sleep(100 * time.Millisecond)
	stopRconWatchdog()
}

func TestRconWatchdogConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			setRconWatchdogConnected(true)
			setRconWatchdogConnected(false)
		}()
	}
	wg.Wait()
}