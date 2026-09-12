package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"github.com/gokayybaz/bazusop/internal/agent"
	"github.com/gokayybaz/bazusop/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "sürüm ve build kimliğini göster")
	flag.Parse()
	buildIdentity := version.Current()
	if *showVersion {
		fmt.Printf("bazUSOP agent %s (%s, %s)\n", buildIdentity.Version, buildIdentity.Commit, buildIdentity.BuildDate)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	configuration := agent.LoadConfig()
	if err := configuration.Validate(); err != nil {
		logger.Error("invalid agent configuration", "error", err)
		os.Exit(1)
	}
	hostname, err := os.Hostname()
	if err != nil {
		logger.Error("could not read hostname", "error", err)
		os.Exit(1)
	}
	client, err := agent.NewClient(configuration, agent.NewIdentityStore(configuration.StateDir))
	if err != nil {
		logger.Error("could not initialize agent client", "error", err)
		os.Exit(1)
	}
	runner := agent.Runner{
		Hub: client, Collector: agent.NewCollector(buildIdentity.Version),
		ReportInterval: configuration.ReportInterval, Logger: logger,
		Hostname: hostname, OperatingSystem: runtime.GOOS,
	}
	if err := runPlatform(runner); err != nil {
		logger.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}
