package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gokayybaz/bazusop/internal/alerting"
	"github.com/gokayybaz/bazusop/internal/audit"
	"github.com/gokayybaz/bazusop/internal/audittrail"
	"github.com/gokayybaz/bazusop/internal/cloudinventory"
	"github.com/gokayybaz/bazusop/internal/config"
	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/inventory"
	"github.com/gokayybaz/bazusop/internal/jobs"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/server"
	"github.com/gokayybaz/bazusop/internal/serviceinventory"
	postgresstore "github.com/gokayybaz/bazusop/internal/storage/postgres"
	"github.com/gokayybaz/bazusop/internal/telemetry"
	"github.com/gokayybaz/bazusop/internal/tenancy"
	"github.com/gokayybaz/bazusop/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "sürüm ve build kimliğini göster")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.Current())
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	configuration := config.Load()
	if err := configuration.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	defaultScope := tenancy.DefaultScope()
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
	var authority *enrollment.Authority
	var inventoryStore inventory.Store = inventory.NewMemoryStore()
	var telemetryStore telemetry.Store = telemetry.NewMemoryStore()
	var serviceInventoryStore serviceinventory.Store = serviceinventory.NewMemoryStore()
	var logStore logstream.Store = logstream.NewMemoryStore()
	jobMemoryStore := jobs.NewMemoryStore()
	var jobStore jobs.Store = jobMemoryStore
	alertMemoryStore := alerting.NewMemoryStore()
	var alertStore alerting.Store = alertMemoryStore
	var cloudInventoryStore cloudinventory.Store = cloudinventory.NewMemoryStore()
	var auditStore audit.Store = audit.NewMemoryStore(jobMemoryStore, alertMemoryStore)
	var auditTrailStore audittrail.Store = audittrail.NewMemoryStore()
	storageMode := "memory"
	if configuration.DatabaseURL != "" {
		startupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		postgresStore, err := postgresstore.Open(
			startupContext,
			configuration.DatabaseURL,
			postgresstore.WithTimescale(configuration.TimescaleEnabled),
			postgresstore.WithRetention(configuration.TelemetryRetentionDays, configuration.LogRetentionDays),
		)
		if err != nil {
			cancel()
			logger.Error("could not initialize PostgreSQL inventory store", "error", err)
			os.Exit(1)
		}
		authority, err = enrollment.NewPersistentAuthority(startupContext, bootstrapToken, defaultScope, postgresStore)
		cancel()
		if err != nil {
			postgresStore.Close()
			logger.Error("could not initialize persistent agent certificate authority", "error", err)
			os.Exit(1)
		}
		defer postgresStore.Close()
		inventoryStore = postgresStore
		telemetryStore = postgresStore
		serviceInventoryStore = postgresStore
		logStore = postgresStore
		jobStore = postgresStore
		alertStore = postgresStore
		cloudInventoryStore = postgresStore
		auditStore = postgresStore
		auditTrailStore = postgresStore
		storageMode = "postgresql"
	} else {
		logger.Warn("DATABASE_URL is not set; inventory will be stored in memory")
	}
	if authority == nil {
		var err error
		authority, err = enrollment.NewAuthority(bootstrapToken)
		if err != nil {
			logger.Error("could not initialize agent certificate authority", "error", err)
			os.Exit(1)
		}
	}
	inventoryService := inventory.NewService(inventoryStore)
	telemetryService := telemetry.NewService(telemetryStore)
	serviceInventoryService := serviceinventory.NewService(serviceInventoryStore)
	logService := logstream.NewService(logStore)
	jobService, err := jobs.NewService(jobStore, jobs.WithSigningKey(authority.JobSigningKey()), jobs.WithHostScopeChecker(inventoryService.HasHost))
	if err != nil {
		logger.Error("could not initialize job signing authority", "error", err)
		os.Exit(1)
	}
	alertService, err := alerting.NewService(alertStore, alerting.WithHostScopeChecker(inventoryService.HasHost))
	if err != nil {
		logger.Error("could not initialize alert service", "error", err)
		os.Exit(1)
	}
	cloudInventoryService := cloudinventory.NewService(cloudInventoryStore, inventoryService)
	auditService := audit.NewService(auditStore)
	auditTrailService := audittrail.NewService(auditTrailStore)
	buildIdentity := version.Current()
	if configuration.OperatorToken == "" && configuration.AdminToken == "" {
		logger.Warn("BAZUSOP_OPERATOR_TOKEN and BAZUSOP_ADMIN_TOKEN are not set; authorized mutations are disabled")
	}
	if configuration.AdminToken == "" {
		logger.Warn("BAZUSOP_ADMIN_TOKEN is not set; operator token retains administrative access for compatibility")
	}
	httpServer := &http.Server{
		Addr: configuration.HTTPAddress,
		Handler: server.NewHandler(
			server.WithDefaultScope(defaultScope),
			server.WithEnrollment(authority),
			server.WithInventory(inventoryService),
			server.WithTelemetry(telemetryService),
			server.WithServiceInventory(serviceInventoryService),
			server.WithLogs(logService),
			server.WithJobs(jobService, configuration.OperatorToken),
			server.WithAlerts(alertService, configuration.OperatorToken),
			server.WithCloudInventory(cloudInventoryService, configuration.OperatorToken),
			server.WithAudit(auditService),
			server.WithAuditTrail(auditTrailService),
			server.WithAdminToken(configuration.AdminToken),
			server.WithRuntimeConfiguration(server.RuntimeConfiguration{
				Storage: storageMode, TimescaleEnabled: configuration.TimescaleEnabled && storageMode == "postgresql",
				TelemetryRetentionDays: configuration.TelemetryRetentionDays,
				LogRetentionDays:       configuration.LogRetentionDays,
				Version:                buildIdentity.Version,
				Commit:                 buildIdentity.Commit,
				BuildDate:              buildIdentity.BuildDate,
			}),
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
	go evaluateReachability(shutdownSignal, logger, inventoryService, alertService, defaultScope)

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

func evaluateReachability(ctx context.Context, logger *slog.Logger, inventoryService *inventory.Service, alertService *alerting.Service, scope tenancy.Scope) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hosts, err := inventoryService.List(ctx, scope)
			if err != nil {
				logger.Error("could not evaluate reachability alerts", "error", err)
				continue
			}
			for _, host := range hosts {
				if err := alertService.EvaluateReachability(ctx, host.Scope(), host.AgentID, host.LastSeenAt); err != nil {
					logger.Error("could not evaluate host reachability", "agent_id", host.AgentID, "error", err)
				}
			}
		}
	}
}
