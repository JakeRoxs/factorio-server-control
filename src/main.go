package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/OpenFactorioServerManager/factorio-server-manager/api"
	"github.com/OpenFactorioServerManager/factorio-server-manager/bootstrap"
	"github.com/OpenFactorioServerManager/factorio-server-manager/factorio"
)

const (
	httpShutdownTimeout     = 10 * time.Second
	factorioShutdownTimeout = 165 * time.Second
	shutdownPollInterval    = 100 * time.Millisecond
	httpReadHeaderTimeout   = 10 * time.Second
	httpIdleTimeout         = 60 * time.Second
	httpMaximumHeaderBytes  = 1 * 1024 * 1024
)

func main() {
	// get the all configs based on the flags
	config := bootstrap.NewConfig(os.Args[1:])

	// setup the required files for the mods
	factorio.ModStartUp()

	// Initialize Factorio Server struct
	err := factorio.NewFactorioServer()
	if err != nil {
		log.Printf("Error occurred during Server initialization: %v\n", err)
		return
	}
	if err := factorio.InitializeProfiles(); err != nil {
		log.Printf("Error initializing Factorio profiles: %v\n", err)
		return
	}
	if err := factorio.MigrateLegacyProfileCheckpoints(); err != nil {
		log.Printf("Warning: error migrating legacy checkpoint stores: %v\n", err)
	}
	if _, err := factorio.InitServerManager(); err != nil {
		log.Printf("Error initializing server manager: %v\n", err)
		return
	}
	factorio.StartConfiguredAutostart()
	factorio.StartMapSnapshotScheduler()

	// Initialize authentication system
	api.SetupAuth()

	// Initialize HTTP router -- also initializes websocket
	router := api.NewRouter()

	address := config.ServerIP + ":" + config.ServerPort
	httpServer := &http.Server{
		Addr:              address,
		Handler:           router,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaximumHeaderBytes,
	}
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- httpServer.ListenAndServe()
	}()

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdownSignal)

	log.Printf("Starting server on: %s", address)
	select {
	case err := <-serverResult:
		unexpected := err != nil && !errors.Is(err, http.ErrServerClosed)
		if unexpected {
			log.Printf("Manager HTTP server stopped unexpectedly: %v", err)
		}
		shutdownFactorioServer(factorioShutdownTimeout)
		if unexpected {
			os.Exit(1)
		}
	case received := <-shutdownSignal:
		// Restore the default behavior so a second signal can force termination.
		signal.Stop(shutdownSignal)
		log.Printf("Received %s; shutting down the manager and Factorio", received)
		shutdownHTTPServer(httpServer)
		shutdownFactorioServer(factorioShutdownTimeout)
	}

}

func shutdownHTTPServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Manager HTTP shutdown did not complete cleanly: %v", err)
	}
}

func shutdownFactorioServer(timeout time.Duration) {
	if manager := factorio.GetServerManager(); manager != nil {
		servers := manager.ListServers()
		processes := make([]shutdownProcess, 0, len(servers))
		for _, server := range servers {
			processes = append(processes, server)
		}
		shutdownFactorioProcesses(processes, timeout)
		return
	}
	shutdownFactorioProcess(factorio.GetFactorioServer(), timeout)
}

func shutdownFactorioProcesses(servers []shutdownProcess, timeout time.Duration) {
	var pending sync.WaitGroup
	for _, server := range servers {
		pending.Add(1)
		go func() {
			defer pending.Done()
			shutdownFactorioProcess(server, timeout)
		}()
	}
	pending.Wait()
}

type shutdownProcess interface {
	IsBusy() bool
	IsStopping() bool
	Stop() error
}

func shutdownFactorioProcess(server shutdownProcess, timeout time.Duration) {
	// Startup is also busy: a process may not have been published as running
	// yet, but still must be stopped before its manager exits.
	if !server.IsBusy() {
		return
	}

	deadline := time.Now().Add(timeout)
	var stopResult <-chan error
	if !server.IsStopping() {
		result := make(chan error, 1)
		stopResult = result
		go func() {
			result <- server.Stop()
		}()
	}

	remaining := time.Until(deadline)
	if remaining <= 0 {
		log.Printf("Factorio did not stop within %s; container runtime may force termination", timeout)
		return
	}
	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()
	timeoutTimer := time.NewTimer(remaining)
	defer timeoutTimer.Stop()
	for server.IsBusy() {
		select {
		case err := <-stopResult:
			stopResult = nil
			if err != nil {
				log.Printf("Unable to request a clean Factorio shutdown: %v", err)
			}
		case <-ticker.C:
		case <-timeoutTimer.C:
			log.Printf("Factorio did not stop within %s; container runtime may force termination", timeout)
			return
		}
	}
	log.Printf("Factorio stopped cleanly")

}
