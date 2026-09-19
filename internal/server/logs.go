package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gokayybaz/bazusop/internal/enrollment"
	"github.com/gokayybaz/bazusop/internal/logstream"
	"github.com/gokayybaz/bazusop/internal/tenancy"
)

func WithLogs(service *logstream.Service) Option {
	return func(options *handlerOptions) {
		options.logService = service
	}
}

func handleLogIngest(authority *enrollment.Authority, service *logstream.Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		agent, ok := authenticateAgent(response, request, authority)
		if !ok {
			return
		}
		var batch logstream.Batch
		if err := decodeJSON(response, request, &batch); err != nil {
			return
		}
		if err := service.Ingest(request.Context(), agent, batch); errors.Is(err, logstream.ErrInvalidLogs) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}
}

func handleSearchLogs(service *logstream.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		to := time.Now().UTC()
		from := to.Add(-time.Hour)
		limit := 100
		var err error
		if value := request.URL.Query().Get("from"); value != "" {
			from, err = time.Parse(time.RFC3339, value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		if value := request.URL.Query().Get("to"); value != "" {
			to, err = time.Parse(time.RFC3339, value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		if value := request.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
			if err != nil {
				http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
		}
		entries, err := service.Search(request.Context(), logstream.Query{Scope: scope,
			AgentID: request.PathValue("agentID"), From: from, To: to,
			Collector: logstream.Collector(request.URL.Query().Get("collector")),
			Severity:  logstream.Severity(request.URL.Query().Get("severity")),
			Source:    request.URL.Query().Get("source"), Text: request.URL.Query().Get("q"), Limit: limit,
		})
		if errors.Is(err, logstream.ErrInvalidLogs) {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Entries []logstream.Entry `json:"entries"`
		}{Entries: entries})
	}
}

func handleStreamLogs(service *logstream.Service, scope tenancy.Scope) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		flusher, ok := response.(http.Flusher)
		if !ok {
			http.Error(response, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
			return
		}
		stream, err := service.Subscribe(request.Context(), scope, request.PathValue("agentID"))
		if err != nil {
			http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		response.Header().Set("Cache-Control", "no-cache")
		response.Header().Set("X-Accel-Buffering", "no")
		_, _ = fmt.Fprint(response, "event: ready\ndata: {}\n\n")
		flusher.Flush()
		for {
			select {
			case <-request.Context().Done():
				return
			case entry := <-stream:
				payload, err := json.Marshal(entry)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(response, "event: log\ndata: %s\n\n", payload); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
