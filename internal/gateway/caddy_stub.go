package gateway

import (
	"encoding/json"
	"net/http"
	"time"
)

var startTime = time.Now()

type HealthResponse struct {
	Status    string `json:"status"`
	Service   string `json:"service"`
	Version   string `json:"version"`
	Uptime    string `json:"uptime"`
	Timestamp int64  `json:"timestamp"`
}

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	resp := HealthResponse{
		Status:    "ok",
		Service:   "SILA Social Integration & Linked Accounts",
		Version:   "1.0.0",
		Uptime:    time.Since(startTime).String(),
		Timestamp: time.Now().Unix(),
	}
	json.NewEncoder(w).Encode(resp)
}
