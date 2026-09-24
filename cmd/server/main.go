package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"link-proxy/internal/dashboard"
	"link-proxy/internal/database"
	"link-proxy/internal/proxy"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := getEnv("PORT", "8080")
	dbPath := getEnv("DB_PATH", "proxy.db")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	baseURL := getEnv("BASE_URL", "http://localhost:"+port)
	smallLimit, _ := strconv.ParseInt(getEnv("SMALL_FILE_LIMIT", "2097152"), 10, 64) // 2MB

	// 1. Initialize SQLite with PRAGMA optimizations
	db, err := database.Open(dbPath)
	if err != nil {
		slog.Error("Failed to initialize SQLite database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("SQLite database initialized", "path", dbPath)

	// 2. Initialize Proxy Service
	proxySvc := proxy.NewProxyService(db, redisAddr, baseURL, smallLimit)

	// 3. Initialize Dashboard Handler
	dashHandler, err := dashboard.NewHandler(db, baseURL)
	if err != nil {
		slog.Error("Failed to initialize dashboard handler", "error", err)
		os.Exit(1)
	}

	// 4. Setup Router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)

	trustedIPHeader := getEnv("TRUSTED_CLIENT_IP_HEADER", "")
	if trustedIPHeader != "" {
		r.Use(middleware.ClientIPFromHeader(trustedIPHeader))
	} else {
		r.Use(middleware.ClientIPFromRemoteAddr)
	}

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Healthcheck
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Streaming Proxy
	r.Get("/p/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		proxySvc.ServeStream(w, r, id)
	})

	// API: Register URL
	r.Post("/api/proxy", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
			http.Error(w, `{"error":"Invalid request payload; 'url' is required"}`, http.StatusBadRequest)
			return
		}

		resp, err := proxySvc.RegisterURL(r.Context(), req.URL)
		if err != nil {
			slog.Warn("Failed to register URL", "url", req.URL, "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	adminUser := getEnv("ADMIN_USER", "admin")
	adminPass := getEnv("ADMIN_PASS", "admin")

	// Dashboard UI & Endpoints (protected with Basic Auth)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})

	r.Group(func(admin chi.Router) {
		admin.Use(middleware.BasicAuth("LinkProxy Dashboard", map[string]string{
			adminUser: adminPass,
		}))
		admin.Get("/dashboard", dashHandler.ServeDashboard)
		admin.Get("/docs", dashHandler.ServeDocs)
		admin.Get("/api/dashboard/stats", dashHandler.ServeStats)
		admin.Get("/api/dashboard/files", dashHandler.ServeFiles)
	})

	// 5. Server Lifecycle & Graceful Shutdown
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second, // allow large streaming
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("Server listening", "port", port, "url", baseURL)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced shutdown", "error", err)
	}
	slog.Info("Server stopped cleanly")
}
