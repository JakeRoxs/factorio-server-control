package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type shutdownFixture struct {
	busy     atomic.Bool
	stopping bool
	stops    atomic.Int32
	onStop   func() error
}

func (s *shutdownFixture) IsBusy() bool     { return s.busy.Load() }
func (s *shutdownFixture) IsStopping() bool { return s.stopping }
func (s *shutdownFixture) Stop() error {
	s.stops.Add(1)
	if s.onStop != nil {
		return s.onStop()
	}
	return nil
}

func TestShutdownSkipsStoppedRuntime(t *testing.T) {
	server := &shutdownFixture{}
	shutdownFactorioProcess(server, time.Second)
	if server.stops.Load() != 0 {
		t.Fatal("stopped runtime received another stop")
	}
}

func TestShutdownRequestsEveryRuntimeBeforeWaiting(t *testing.T) {
	first := &shutdownFixture{}
	second := &shutdownFixture{}
	first.busy.Store(true)
	second.busy.Store(true)
	requested := make(chan struct{}, 2)
	finish := make(chan struct{})
	done := make(chan struct{})
	t.Cleanup(func() {
		close(finish)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("shutdown workers did not finish after runtimes stopped")
		}
	})
	for _, server := range []*shutdownFixture{first, second} {
		server.onStop = func() error {
			requested <- struct{}{}
			<-finish
			server.busy.Store(false)
			return nil
		}
	}
	go func() {
		shutdownFactorioProcesses([]shutdownProcess{first, second}, 2*time.Second)
		close(done)
	}()
	for range 2 {
		select {
		case <-requested:
		case <-time.After(time.Second):
			t.Fatal("a pending stop prevented another runtime from receiving shutdown")
		}
	}
	select {
	case <-done:
		t.Fatal("shutdown returned while owned runtimes were still busy")
	default:
	}
}

func TestShutdownWaitsForPendingStartupAndStopCompletion(t *testing.T) {
	server := &shutdownFixture{}
	server.busy.Store(true)
	requested := make(chan struct{})
	finish := make(chan struct{})
	server.onStop = func() error { close(requested); <-finish; server.busy.Store(false); return nil }
	done := make(chan struct{})
	go func() { shutdownFactorioProcess(server, 2*time.Second); close(done) }()
	select {
	case <-requested:
	case <-time.After(time.Second):
		close(finish)
		t.Fatal("pending startup did not receive stop")
	}
	select {
	case <-done:
		close(finish)
		t.Fatal("shutdown returned before the process stopped")
	default:
	}
	close(finish)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not observe process completion")
	}
	if server.stops.Load() != 1 {
		t.Fatal("shutdown requested stop more than once")
	}
}

func TestShutdownAlreadyStoppingAndStopErrorsRemainBounded(t *testing.T) {
	for _, stopping := range []bool{true, false} {
		t.Run(map[bool]string{true: "already stopping", false: "stop error"}[stopping], func(t *testing.T) {
			server := &shutdownFixture{stopping: stopping, onStop: func() error { return errors.New("synthetic stop failure") }}
			server.busy.Store(true)
			start := time.Now()
			shutdownFactorioProcess(server, 25*time.Millisecond)
			if time.Since(start) > time.Second {
				t.Fatal("shutdown exceeded its bounded grace period")
			}
			if !server.IsBusy() {
				t.Fatal("timeout must not pretend the process exited")
			}
			if stopping && server.stops.Load() != 0 {
				t.Fatal("duplicate stop while shutdown was pending")
			}
		})
	}
}
