// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

//go:embed web/index.html
var webFiles embed.FS

type dashboardServer struct{ app *App }

func (s *dashboardServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("PUT /api/config", s.config)
	mux.HandleFunc("PUT /api/assumptions", s.assumptions)
	mux.HandleFunc("POST /api/start", s.start)
	mux.HandleFunc("POST /api/pause", s.pause)
	mux.HandleFunc("POST /api/stop", s.stop)
	mux.HandleFunc("POST /api/reset-history", s.resetHistory)
	return securityHeaders(mux)
}

func (s *dashboardServer) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	content, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "dashboard asset is unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(content)
}

func (s *dashboardServer) state(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.app.State())
}

func (s *dashboardServer) config(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	var config SimulatorConfig
	if err := decodeJSON(w, r, &config); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.SetConfig(config); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, s.app.State())
}

func (s *dashboardServer) assumptions(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	var assumptions EstimateAssumptions
	if err := decodeJSON(w, r, &assumptions); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.SetAssumptions(assumptions); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, s.app.State())
}

func (s *dashboardServer) start(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	var request struct {
		Confirmation string `json:"confirmation"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.Start(r.Context(), request.Confirmation); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusAccepted, s.app.State())
}

func (s *dashboardServer) pause(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	s.app.Pause()
	writeJSON(w, http.StatusAccepted, s.app.State())
}

func (s *dashboardServer) stop(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	s.app.Stop()
	writeJSON(w, http.StatusAccepted, s.app.State())
}

func (s *dashboardServer) resetHistory(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	s.app.ResetHistory()
	writeJSON(w, http.StatusOK, s.app.State())
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	if r.Body == nil {
		return errors.New("request body is required")
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("request must contain exactly one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsed.Host == r.Host && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func waitForShutdown(server *http.Server, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = server.Shutdown(ctx)
}
