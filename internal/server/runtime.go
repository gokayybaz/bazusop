package server

import "net/http"

type RuntimeConfiguration struct {
	Storage                string `json:"storage"`
	TimescaleEnabled       bool   `json:"timescale_enabled"`
	TelemetryRetentionDays int    `json:"telemetry_retention_days"`
	LogRetentionDays       int    `json:"log_retention_days"`
	Version                string `json:"version"`
	Commit                 string `json:"commit"`
	BuildDate              string `json:"build_date"`
}

func WithRuntimeConfiguration(configuration RuntimeConfiguration) Option {
	return func(options *handlerOptions) {
		options.runtimeConfiguration = &configuration
	}
}

func handleRuntimeConfiguration(configuration RuntimeConfiguration) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, configuration)
	}
}
