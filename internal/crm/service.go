package crm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"sila/internal/database"
	"sila/internal/meta"
)

type Service struct {
	repo       *database.Repository
	metaClient *meta.Client
	hub        *Hub
}

func NewService(repo *database.Repository, metaClient *meta.Client, hub *Hub) *Service {
	return &Service{
		repo:       repo,
		metaClient: metaClient,
		hub:        hub,
	}
}

// GetConversations returns conversation threads for an account
func (s *Service) GetConversations(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	convs, err := s.repo.GetConversations(accountID, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    convs,
	})
}

// GetMessages returns message history for a conversation thread
func (s *Service) GetMessages(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("conversation_id")
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	msgs, err := s.repo.GetMessages(convID, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    msgs,
	})
}

type SendRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
	MediaURL       string `json:"media_url,omitempty"`
}

// SendMessage sends an outbound reply to customer via Meta Graph API
func (s *Service) SendMessage(w http.ResponseWriter, r *http.Request) {
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	conv, err := s.repo.GetConversationByID(req.ConversationID)
	if err != nil || conv == nil {
		http.Error(w, `{"error": "Percakapan tidak ditemukan"}`, http.StatusNotFound)
		return
	}

	acc, err := s.repo.GetAccountByID(conv.AccountID)
	if err != nil || acc == nil {
		http.Error(w, `{"error": "Akun terkait tidak ditemukan"}`, http.StatusNotFound)
		return
	}

	// 1. Send via Meta Graph API
	var sendErr error
	var extMsgID string

	if conv.Platform == "facebook" || conv.Platform == "instagram" {
		endpoint := "/me/messages"
		payload := map[string]interface{}{
			"recipient": map[string]string{
				"id": conv.CustomerID,
			},
			"message": map[string]interface{}{
				"text": req.Message,
			},
		}

		if req.MediaURL != "" {
			payload["message"] = map[string]interface{}{
				"attachment": map[string]interface{}{
					"type": "image",
					"payload": map[string]string{
						"url": req.MediaURL,
					},
				},
			}
		}

		body, err := s.metaClient.Post(endpoint, payload, acc.AccessToken)
		if err != nil {
			sendErr = err
		} else {
			var resp struct {
				MessageID string `json:"message_id"`
			}
			json.Unmarshal(body, &resp)
			extMsgID = resp.MessageID
		}
	} else if conv.Platform == "comment" {
		// Reply to comment
		endpoint := fmt.Sprintf("/%s/comments", conv.ExternalConvID)
		params := url.Values{}
		params.Set("message", req.Message)

		body, err := s.metaClient.Post(endpoint, params, acc.AccessToken)
		if err != nil {
			sendErr = err
		} else {
			var resp struct {
				ID string `json:"id"`
			}
			json.Unmarshal(body, &resp)
			extMsgID = resp.ID
		}
	}

	if sendErr != nil {
		http.Error(w, fmt.Sprintf(`{"error": "Gagal mengirim pesan ke platform: %s"}`, sendErr.Error()), http.StatusInternalServerError)
		return
	}

	// 2. Save outbound message to local database
	msgID := fmt.Sprintf("out_%d", time.Now().UnixNano())
	msg := &database.Message{
		ID:             msgID,
		ConversationID: conv.ID,
		ExternalMsgID:  extMsgID,
		SenderType:     "agent",
		MessageType:    "text",
		Content:        req.Message,
		MediaURL:       req.MediaURL,
		Status:         "sent",
		Timestamp:      time.Now().Unix(),
	}
	_ = s.repo.InsertMessage(msg)

	// Update conversation last message
	conv.LastMessageText = req.Message
	conv.LastMessageTime = time.Now().Unix()
	_ = s.repo.UpsertConversation(conv)

	// Broadcast via WebSocket
	s.hub.BroadcastAll(map[string]interface{}{
		"event":           "message_sent",
		"conversation_id": conv.ID,
		"message":         msg,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    msg,
	})
}
