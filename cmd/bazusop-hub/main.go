package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gokayybaz/bazusop/internal/config"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	postgresstore "github.com/gokayybaz/bazusop/internal/storage/postgres"
	"github.com/gokayybaz/bazusop/internal/telemetry"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	configuration := config.Load()
	bootstrapToken := configuration.EnrollmentToken
	if bootstrapToken == "" {
		var err error
		bootstrapToken, err = enrollment.GenerateBootstrapToken()
		if err != nil {
			logger.Error("could not create enrollment token", "error", err)
			os.Exit(1)
		}
		logger.Warn("generated ephemeral enrollment token; set BAZUSOP_ENROLLMENT_TOKEN for a stable bootstrap token", "token", bootstrapToken)
	}
	authority, err := enrollment.NewAuthority(bootstrapToken)
	if err != nil {
		logger.Error("could not initialize agent certificate authority", "error", err)
		os.Exit(1)
	}
	var inventoryStore inventory.Store = inventory.NewMemoryStore()
	var telemetryStore telemetry.Store = telemetry.NewMemoryStore()
	var serviceInventoryStore serviceinventory.Store = serviceinventory.NewMemoryStore()
	var logStore logstream.Store = logstream.NewMemoryStore()
	if configuration.DatabaseURL != "" {
		startupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		postgresStore, err := postgresstore.Open(
			startupContext,
			configuration.DatabaseURL,
			postgresstore.WithTimescale(configuration.TimescaleEnabled),
		)
		cancel()
		if err != nil {
			logger.Error("could not initialize PostgreSQL inventory store", "error", err)
			os.Exit(1)
		}
		defer postgresStore.Close()
		inventoryStore = postgresStore
		telemetryStore = postgresStore
		serviceInventoryStore = postgresStore
		logStore = postgresStore
	} else {
		logger.Warn("DATABASE_URL is not set; inventory will be stored in memory")
	}
	inventoryService := inventory.NewService(inventoryStore)
	telemetryService := telemetry.NewService(telemetryStore)
	serviceInventoryService := serviceinventory.NewService(serviceInventoryStore)
	logService := logstream.NewService(logStore)
	httpServer := &http.Server{
		Addr: configuration.HTTPAddress,
		Handler: server.NewHandler(
			server.WithEnrollment(authority),
			server.WithInventory(inventoryService),
			server.WithTelemetry(telemetryService),
			server.WithServiceInventory(serviceInventoryService),
			server.WithLogs(logService),
		),
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ClientAuth: tls.VerifyClientCertIfGiven,
			ClientCAs:  authority.ClientCAPool(),
		},
	}
	if (configuration.TLSCertificate == "") != (configuration.TLSPrivateKey == "") {
		logger.Error("BAZUSOP_TLS_CERT_FILE and BAZUSOP_TLS_KEY_FILE must be configured together")
		os.Exit(1)
	}

	shutdownSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("bazUSOP hub listening", "address", httpServer.Addr)
		var serveError error
		if configuration.TLSCertificate != "" {
			serveError = httpServer.ListenAndServeTLS(configuration.TLSCertificate, configuration.TLSPrivateKey)
		} else {
			serveError = httpServer.ListenAndServe()
		}
		if serveError != nil && !errors.Is(serveError, http.ErrServerClosed) {
			logger.Error("hub stopped unexpectedly", "error", serveError)
			os.Exit(1)
		}
	}()

	<-shutdownSignal.Done()

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}
