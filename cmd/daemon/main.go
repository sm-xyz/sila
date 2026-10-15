package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sila/config"
	"sila/internal/crm"
	"sila/internal/database"
	"sila/internal/gateway"
	"sila/internal/meta"
	"sila/internal/scheduler"
	"sila/internal/youtube"
)

func main() {
	log.Println("==========================================================")
	log.Println("   SILA (Social Integration & Linked Accounts) Daemon     ")
	log.Println("==========================================================")

	cfg := config.LoadConfig()

	// 1. Inisialisasi Database SQLite WAL
	dbMgr, err := database.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("[SILA Fatal] Gagal menginisialisasi SQLite: %v", err)
	}
	defer dbMgr.Close()

	repo := database.NewRepository(dbMgr)

	// 2. Inisialisasi Client & Manager Ekosistem
	metaClient := meta.NewClient(cfg.MetaAppID, cfg.MetaAppSecret)
	metaOAuth := meta.NewOAuthManager(cfg, metaClient, repo)
	metaPub := meta.NewPublisher(metaClient)

	ytClient := youtube.NewClient()
	ytOAuth := youtube.NewOAuthManager(cfg, ytClient, repo)
	ytUploader := youtube.NewUploader(ytClient, ytOAuth)

	// 3. Inisialisasi CRM & Realtime WebSocket Hub
	hub := crm.NewHub()
	crmService := crm.NewService(repo, metaClient, hub)

	// Webhook Meta
	metaWebhook := meta.NewWebhookHandler(cfg, repo, func(msg interface{}) {
		hub.BroadcastAll(msg)
	})

	// 4. Scheduler Worker Background
	workerPool := scheduler.NewWorkerPool(cfg.MaxConcurrency, repo, metaPub, ytUploader)
	cronWorker := scheduler.NewCronWorker(repo, workerPool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cronWorker.Start(ctx)

	// 5. Middleware Gateway
	mw := gateway.NewMiddleware(cfg.SecretKey)

	// 6. Router Setup
	mux := http.NewServeMux()

	// Compliance & Health
	mux.HandleFunc("/health", gateway.HandleHealth)
	mux.HandleFunc("/privacy", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "/opt/sila/public/privacy.html")
	})
	mux.HandleFunc("/deletion", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "/opt/sila/public/deletion.html")
	})

	// OAuth Endpoints (Dibuka di popup browser pengguna)
	mux.HandleFunc("/auth/meta/login", func(w http.ResponseWriter, r *http.Request) {
		uid := gateway.GetUserIDFromRequest(r)
		platform := r.URL.Query().Get("platform")
		if platform == "" {
			platform = "facebook"
		}
		redirect := r.URL.Query().Get("redirect")
		authURL := metaOAuth.BuildAuthURL(uid, platform, redirect)
		http.Redirect(w, r, authURL, http.StatusFound)
	})
	mux.HandleFunc("/auth/meta/callback", metaOAuth.HandleCallback)

	mux.HandleFunc("/auth/youtube/login", func(w http.ResponseWriter, r *http.Request) {
		uid := gateway.GetUserIDFromRequest(r)
		redirect := r.URL.Query().Get("redirect")
		authURL := ytOAuth.BuildAuthURL(uid, redirect)
		http.Redirect(w, r, authURL, http.StatusFound)
	})
	mux.HandleFunc("/auth/youtube/callback", ytOAuth.HandleCallback)

	// Meta Webhook Ingestion
	mux.HandleFunc("/webhook/meta", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			metaWebhook.HandleVerify(w, r)
		} else if r.Method == http.MethodPost {
			metaWebhook.HandleEvent(w, r)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})

	// Real-time WebSocket
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		uid := gateway.GetUserIDFromRequest(r)
		hub.Upgrade(w, r, uid)
	})

	// Protected REST API Endpoints (Memerlukan X-SILA-SECRET)
	// Accounts
	mux.HandleFunc("/api/accounts", mw.AuthRequired(func(w http.ResponseWriter, r *http.Request) {
		uid := gateway.GetUserIDFromRequest(r)
		if r.Method == http.MethodGet {
			var accs []database.Account
			var err error
			if uid > 0 {
				accs, err = repo.GetAccountsByUserID(uid)
			} else {
				accs, err = repo.GetAllAccounts()
			}
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data":    accs,
			})
			return
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}))

	mux.HandleFunc("/api/accounts/", mw.AuthRequired(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 4 {
				accID := parts[3]
				uid := gateway.GetUserIDFromRequest(r)
				if err := repo.DeleteAccount(accID, uid); err != nil {
					http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"message": "Akun berhasil diputuskan",
				})
				return
			}
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}))

	// Posts Submission & History
	mux.HandleFunc("/api/posts/submit", mw.AuthRequired(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Accounts    []string `json:"accounts"`
			PostType    string   `json:"post_type"`
			Caption     string   `json:"caption"`
			Title       string   `json:"title"`
			MediaURLs   []string `json:"media_urls"`
			ScheduledAt int64    `json:"scheduled_at"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error": "Invalid request body"}`, http.StatusBadRequest)
			return
		}

		uid := gateway.GetUserIDFromRequest(r)
		if payload.ScheduledAt == 0 {
			payload.ScheduledAt = time.Now().Unix()
		}

		mediaPathsJSON, _ := json.Marshal(payload.MediaURLs)
		var insertedIDs []string

		for _, accID := range payload.Accounts {
			acc, err := repo.GetAccountByID(accID)
			if err != nil || acc == nil {
				continue
			}

			itemID := fmt.Sprintf("post_%d_%s", time.Now().UnixNano(), acc.Platform)
			item := &database.PostQueueItem{
				ID:          itemID,
				UserID:      uid,
				AccountID:   acc.ID,
				Platform:    acc.Platform,
				PostType:    payload.PostType,
				Caption:     payload.Caption,
				Title:       payload.Title,
				MediaPaths:  string(mediaPathsJSON),
				ScheduledAt: payload.ScheduledAt,
				Status:      "pending",
			}
			if err := repo.InsertPostQueue(item); err == nil {
				insertedIDs = append(insertedIDs, itemID)
				// If scheduled immediately (scheduled_at <= now), submit to worker pool
				if item.ScheduledAt <= time.Now().Unix()+5 {
					workerPool.ProcessPost(item)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("%d postingan berhasil dijadwalkan", len(insertedIDs)),
			"data":    insertedIDs,
		})
	}))

	mux.HandleFunc("/api/posts/history", mw.AuthRequired(func(w http.ResponseWriter, r *http.Request) {
		uid := gateway.GetUserIDFromRequest(r)
		status := r.URL.Query().Get("status")
		history, err := repo.GetPostHistory(uid, status)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    history,
		})
	}))

	mux.HandleFunc("/api/posts/", mw.AuthRequired(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 4 {
				postID := parts[3]
				uid := gateway.GetUserIDFromRequest(r)
				if err := repo.DeletePost(postID, uid); err != nil {
					http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
					"message": "Jadwal postingan berhasil dibatalkan",
				})
				return
			}
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}))

	// Omni CRM Endpoints
	mux.HandleFunc("/api/crm/conversations", mw.AuthRequired(crmService.GetConversations))
	mux.HandleFunc("/api/crm/messages", mw.AuthRequired(crmService.GetMessages))
	mux.HandleFunc("/api/crm/send", mw.AuthRequired(crmService.SendMessage))

	addr := fmt.Sprintf("%s:%s", cfg.Bind, cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mw.CORS(mux),
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	go func() {
		log.Printf("[SILA] Server mendengarkan pada %s (Public URL: %s)", addr, cfg.PublicURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[SILA Fatal] Server error: %v", err)
		}
	}()

	// Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[SILA] Menerima sinyal shutdown, mematikan service secara anggun...")
	cronWorker.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("[SILA Warning] Server forced shutdown: %v", err)
	}

	log.Println("[SILA] Daemon berhasil dimatikan dengan aman.")
}
