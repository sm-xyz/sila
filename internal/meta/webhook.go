package meta

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"sila/config"
	"sila/internal/database"
)

type WebhookHandler struct {
	cfg        *config.Config
	repo       *database.Repository
	broadcaster func(msg interface{})
}

func NewWebhookHandler(cfg *config.Config, repo *database.Repository, broadcaster func(msg interface{})) *WebhookHandler {
	return &WebhookHandler{
		cfg:         cfg,
		repo:        repo,
		broadcaster: broadcaster,
	}
}

// HandleVerify responds to Meta's webhook challenge
func (h *WebhookHandler) HandleVerify(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode == "subscribe" && token == h.cfg.MetaVerifyToken {
		log.Printf("[SILA Meta Webhook] Webhook challenge verified successfully!")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challenge))
		return
	}

	http.Error(w, "Forbidden", http.StatusForbidden)
}

// HandleEvent ingests incoming messages & comments from Facebook & Instagram
func (h *WebhookHandler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Acknowledge early to Meta
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("EVENT_RECEIVED"))

	// Parse payload asynchronously
	go h.processEvent(body)
}

func (h *WebhookHandler) processEvent(body []byte) {
	var payload struct {
		Object string `json:"object"`
		Entry  []struct {
			ID        string `json:"id"`
			Time      int64  `json:"time"`
			Messaging []struct {
				Sender struct {
					ID string `json:"id"`
				} `json:"sender"`
				Recipient struct {
					ID string `json:"id"`
				} `json:"recipient"`
				Timestamp int64 `json:"timestamp"`
				Message   *struct {
					Mid  string `json:"mid"`
					Text string `json:"text"`
					Attachments []struct {
						Type    string `json:"type"`
						Payload struct {
							URL string `json:"url"`
						} `json:"payload"`
					} `json:"attachments"`
				} `json:"message"`
			} `json:"messaging"`
			Changes []struct {
				Field string          `json:"field"`
				Value json.RawMessage `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}

	for _, entry := range payload.Entry {
		accountID := entry.ID

		// 1. Process Messaging (Messenger & IG DMs)
		for _, m := range entry.Messaging {
			if m.Message == nil {
				continue
			}

			// Don't ingest echoes from ourselves
			if m.Sender.ID == accountID {
				continue
			}

			customerID := m.Sender.ID
			msgText := m.Message.Text
			mediaURL := ""
			msgType := "text"

			if len(m.Message.Attachments) > 0 {
				att := m.Message.Attachments[0]
				msgType = att.Type
				mediaURL = att.Payload.URL
			}

			platform := "facebook"
			if payload.Object == "instagram" {
				platform = "instagram"
			}

			convID := fmt.Sprintf("conv_%s_%s", accountID, customerID)
			conv := &database.Conversation{
				ID:              convID,
				AccountID:       accountID,
				Platform:        platform,
				ExternalConvID:  customerID,
				CustomerID:      customerID,
				CustomerName:    "Audiens " + platform,
				LastMessageText: msgText,
				LastMessageTime: m.Timestamp / 1000,
				UnreadCount:     1,
			}
			_ = h.repo.UpsertConversation(conv)

			msgID := fmt.Sprintf("msg_%s", m.Message.Mid)
			msg := &database.Message{
				ID:             msgID,
				ConversationID: convID,
				ExternalMsgID:  m.Message.Mid,
				SenderType:     "customer",
				MessageType:    msgType,
				Content:        msgText,
				MediaURL:       mediaURL,
				Status:         "received",
				Timestamp:      m.Timestamp / 1000,
			}
			_ = h.repo.InsertMessage(msg)

			// Broadcast to connected WebSocket clients
			if h.broadcaster != nil {
				h.broadcaster(map[string]interface{}{
					"event":           "new_message",
					"platform":        platform,
					"account_id":      accountID,
					"conversation_id": convID,
					"customer_id":     customerID,
					"message":         msg,
				})
			}
		}

		// 2. Process Feed / Comments
		for _, ch := range entry.Changes {
			if ch.Field == "feed" || ch.Field == "comments" {
				var commentVal struct {
					Item      string `json:"item"`
					CommentID string `json:"comment_id"`
					Message   string `json:"message"`
					From      struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"from"`
					PostID string `json:"post_id"`
				}
				if json.Unmarshal(ch.Value, &commentVal) == nil && commentVal.Item == "comment" {
					convID := fmt.Sprintf("comment_%s_%s", accountID, commentVal.PostID)
					conv := &database.Conversation{
						ID:              convID,
						AccountID:       accountID,
						Platform:        "comment",
						ExternalConvID:  commentVal.PostID,
						CustomerID:      commentVal.From.ID,
						CustomerName:    commentVal.From.Name,
						LastMessageText: commentVal.Message,
						LastMessageTime: time.Now().Unix(),
						UnreadCount:     1,
					}
					_ = h.repo.UpsertConversation(conv)

					msg := &database.Message{
						ID:             fmt.Sprintf("cmt_%s", commentVal.CommentID),
						ConversationID: convID,
						ExternalMsgID:  commentVal.CommentID,
						SenderType:     "customer",
						MessageType:    "text",
						Content:        commentVal.Message,
						Status:         "received",
						Timestamp:      time.Now().Unix(),
					}
					_ = h.repo.InsertMessage(msg)

					if h.broadcaster != nil {
						h.broadcaster(map[string]interface{}{
							"event":           "new_comment",
							"account_id":      accountID,
							"conversation_id": convID,
							"comment_id":      commentVal.CommentID,
							"message":         msg,
						})
					}
				}
			}
		}
	}
}
