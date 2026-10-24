// use this file only when compiling not windows (all unix systems)
//go:build !windows
// +build !windows

package factorio

import (
	"log"
	"os"
)

// Stubs for windows-only functions

func (server *Server) Stop() error {
	if !server.requestStop() {
		return nil
	}
	if !server.claimStopSignalIfReady() {
		log.Printf("Factorio is still starting; safe shutdown is queued until startup completes")
		return nil
	}
	if server.ID == "" || server.ID == DefaultServerID {
		stopCheckpointMonitor(server.ID)
		runCleanStopCheckpoint(server)
	}
	return server.signalStop()
}

func (server *Server) signalStop() error {
	err := server.Cmd.Process.Signal(os.Interrupt)
	if err != nil {
		server.SetStopping(false)
		if err.Error() == "os: process already finished" {
			server.SetRunning(false)
			return err
		}
		log.Printf("Error sending SIGINT to Factorio process: %s", err)
		return err
	}
	log.Printf("Sent SIGINT to Factorio process. Factorio shutting down...")

	server.closeRcon()

	return nil
}

func (server *Server) checkProcessHealth(text string) {
	// ignore
}
