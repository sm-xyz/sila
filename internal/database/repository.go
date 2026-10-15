package database

import (
	"database/sql"
	"fmt"
	"time"
)

type Account struct {
	ID             string `json:"id"`
	UserID         int64  `json:"user_id"`
	Platform       string `json:"platform"`
	AccountID      string `json:"account_id"`
	AccountName    string `json:"account_name"`
	AccountUsername string `json:"account_username"`
	AvatarURL      string `json:"avatar_url"`
	AccessToken    string `json:"access_token,omitempty"`
	RefreshToken   string `json:"refresh_token,omitempty"`
	TokenExpiresAt int64  `json:"token_expires_at"`
	ExtraMetadata  string `json:"extra_metadata"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type Conversation struct {
	ID              string `json:"id"`
	AccountID       string `json:"account_id"`
	Platform        string `json:"platform"`
	ExternalConvID  string `json:"external_conv_id"`
	CustomerID      string `json:"customer_id"`
	CustomerName    string `json:"customer_name"`
	CustomerAvatar  string `json:"customer_avatar"`
	LastMessageText string `json:"last_message_text"`
	LastMessageTime int64  `json:"last_message_time"`
	UnreadCount     int    `json:"unread_count"`
	IsStarred       int    `json:"is_starred"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

type Message struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	ExternalMsgID  string `json:"external_msg_id"`
	SenderType     string `json:"sender_type"`
	MessageType    string `json:"message_type"`
	Content        string `json:"content"`
	MediaURL       string `json:"media_url"`
	Status         string `json:"status"`
	Timestamp      int64  `json:"timestamp"`
	CreatedAt      int64  `json:"created_at"`
}

type PostQueueItem struct {
	ID               string `json:"id"`
	UserID           int64  `json:"user_id"`
	AccountID        string `json:"account_id"`
	Platform         string `json:"platform"`
	PostType         string `json:"post_type"`
	Caption          string `json:"caption"`
	Title            string `json:"title"`
	MediaPaths       string `json:"media_paths"`
	ScheduledAt      int64  `json:"scheduled_at"`
	Status           string `json:"status"`
	RetryCount       int    `json:"retry_count"`
	ErrorMessage     string `json:"error_message"`
	PublishedPostID  string `json:"published_post_id"`
	PublishedPostURL string `json:"published_post_url"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

type Repository struct {
	mgr *DBManager
}

func NewRepository(mgr *DBManager) *Repository {
	return &Repository{mgr: mgr}
}

// ----------------- ACCOUNTS -----------------

func (r *Repository) UpsertAccount(acc *Account) error {
	r.mgr.Mu.Lock()
	defer r.mgr.Mu.Unlock()

	now := time.Now().Unix()
	if acc.CreatedAt == 0 {
		acc.CreatedAt = now
	}
	acc.UpdatedAt = now

	query := `
	INSERT INTO accounts (id, user_id, platform, account_id, account_name, account_username, avatar_url, access_token, refresh_token, token_expires_at, extra_metadata, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(user_id, platform, account_id) DO UPDATE SET
		account_name=excluded.account_name,
		account_username=excluded.account_username,
		avatar_url=excluded.avatar_url,
		access_token=excluded.access_token,
		refresh_token=excluded.refresh_token,
		token_expires_at=excluded.token_expires_at,
		extra_metadata=excluded.extra_metadata,
		updated_at=excluded.updated_at
	`
	_, err := r.mgr.DB.Exec(query, acc.ID, acc.UserID, acc.Platform, acc.AccountID, acc.AccountName, acc.AccountUsername, acc.AvatarURL, acc.AccessToken, acc.RefreshToken, acc.TokenExpiresAt, acc.ExtraMetadata, acc.CreatedAt, acc.UpdatedAt)
	return err
}

func (r *Repository) GetAccountsByUserID(userID int64) ([]Account, error) {
	r.mgr.Mu.RLock()
	defer r.mgr.Mu.RUnlock()

	query := `SELECT id, user_id, platform, account_id, account_name, account_username, avatar_url, access_token, refresh_token, token_expires_at, extra_metadata, created_at, updated_at FROM accounts WHERE user_id = ? ORDER BY created_at DESC`
	rows, err := r.mgr.DB.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Account
	for rows.Next() {
		var a Account
		var uName, aUrl, rToken, meta sql.NullString
		err := rows.Scan(&a.ID, &a.UserID, &a.Platform, &a.AccountID, &a.AccountName, &uName, &aUrl, &a.AccessToken, &rToken, &a.TokenExpiresAt, &meta, &a.CreatedAt, &a.UpdatedAt)
		if err != nil {
			return nil, err
		}
		a.AccountUsername = uName.String
		a.AvatarURL = aUrl.String
		a.RefreshToken = rToken.String
		a.ExtraMetadata = meta.String
		list = append(list, a)
	}
	return list, nil
}

func (r *Repository) GetAllAccounts() ([]Account, error) {
	r.mgr.Mu.RLock()
	defer r.mgr.Mu.RUnlock()

	query := `SELECT id, user_id, platform, account_id, account_name, account_username, avatar_url, access_token, refresh_token, token_expires_at, extra_metadata, created_at, updated_at FROM accounts ORDER BY created_at DESC`
	rows, err := r.mgr.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Account
	for rows.Next() {
		var a Account
		var uName, aUrl, rToken, meta sql.NullString
		err := rows.Scan(&a.ID, &a.UserID, &a.Platform, &a.AccountID, &a.AccountName, &uName, &aUrl, &a.AccessToken, &rToken, &a.TokenExpiresAt, &meta, &a.CreatedAt, &a.UpdatedAt)
		if err != nil {
			return nil, err
		}
		a.AccountUsername = uName.String
		a.AvatarURL = aUrl.String
		a.RefreshToken = rToken.String
		a.ExtraMetadata = meta.String
		list = append(list, a)
	}
	return list, nil
}

func (r *Repository) GetAccountByID(id string) (*Account, error) {
	r.mgr.Mu.RLock()
	defer r.mgr.Mu.RUnlock()

	query := `SELECT id, user_id, platform, account_id, account_name, account_username, avatar_url, access_token, refresh_token, token_expires_at, extra_metadata, created_at, updated_at FROM accounts WHERE id = ? LIMIT 1`
	var a Account
	var uName, aUrl, rToken, meta sql.NullString
	err := r.mgr.DB.QueryRow(query, id).Scan(&a.ID, &a.UserID, &a.Platform, &a.AccountID, &a.AccountName, &uName, &aUrl, &a.AccessToken, &rToken, &a.TokenExpiresAt, &meta, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	a.AccountUsername = uName.String
	a.AvatarURL = aUrl.String
	a.RefreshToken = rToken.String
	a.ExtraMetadata = meta.String
	return &a, nil
}

func (r *Repository) DeleteAccount(id string, userID int64) error {
	r.mgr.Mu.Lock()
	defer r.mgr.Mu.Unlock()

	var res sql.Result
	var err error
	if userID > 0 {
		res, err = r.mgr.DB.Exec(`DELETE FROM accounts WHERE id = ? AND user_id = ?`, id, userID)
	} else {
		res, err = r.mgr.DB.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	}
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("akun tidak ditemukan atau tidak berhak")
	}
	return nil
}

// ----------------- CRM CONVERSATIONS -----------------

func (r *Repository) UpsertConversation(c *Conversation) error {
	r.mgr.Mu.Lock()
	defer r.mgr.Mu.Unlock()

	now := time.Now().Unix()
	if c.CreatedAt == 0 {
		c.CreatedAt = now
	}
	c.UpdatedAt = now

	query := `
	INSERT INTO crm_conversations (id, account_id, platform, external_conv_id, customer_id, customer_name, customer_avatar, last_message_text, last_message_time, unread_count, is_starred, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(account_id, external_conv_id) DO UPDATE SET
		customer_name=excluded.customer_name,
		customer_avatar=excluded.customer_avatar,
		last_message_text=excluded.last_message_text,
		last_message_time=excluded.last_message_time,
		unread_count=excluded.unread_count,
		is_starred=excluded.is_starred,
		updated_at=excluded.updated_at
	`
	_, err := r.mgr.DB.Exec(query, c.ID, c.AccountID, c.Platform, c.ExternalConvID, c.CustomerID, c.CustomerName, c.CustomerAvatar, c.LastMessageText, c.LastMessageTime, c.UnreadCount, c.IsStarred, c.CreatedAt, c.UpdatedAt)
	return err
}

func (r *Repository) GetConversations(accountID string, limit int) ([]Conversation, error) {
	r.mgr.Mu.RLock()
	defer r.mgr.Mu.RUnlock()

	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `SELECT id, account_id, platform, external_conv_id, customer_id, customer_name, customer_avatar, last_message_text, last_message_time, unread_count, is_starred, created_at, updated_at FROM crm_conversations WHERE account_id = ? ORDER BY last_message_time DESC LIMIT ?`
	rows, err := r.mgr.DB.Query(query, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Conversation
	for rows.Next() {
		var c Conversation
		var cAvatar, lMsg sql.NullString
		err := rows.Scan(&c.ID, &c.AccountID, &c.Platform, &c.ExternalConvID, &c.CustomerID, &c.CustomerName, &cAvatar, &lMsg, &c.LastMessageTime, &c.UnreadCount, &c.IsStarred, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return nil, err
		}
		c.CustomerAvatar = cAvatar.String
		c.LastMessageText = lMsg.String
		list = append(list, c)
	}
	return list, nil
}

func (r *Repository) GetConversationByID(id string) (*Conversation, error) {
	r.mgr.Mu.RLock()
	defer r.mgr.Mu.RUnlock()

	query := `SELECT id, account_id, platform, external_conv_id, customer_id, customer_name, customer_avatar, last_message_text, last_message_time, unread_count, is_starred, created_at, updated_at FROM crm_conversations WHERE id = ? LIMIT 1`
	var c Conversation
	var cAvatar, lMsg sql.NullString
	err := r.mgr.DB.QueryRow(query, id).Scan(&c.ID, &c.AccountID, &c.Platform, &c.ExternalConvID, &c.CustomerID, &c.CustomerName, &cAvatar, &lMsg, &c.LastMessageTime, &c.UnreadCount, &c.IsStarred, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.CustomerAvatar = cAvatar.String
	c.LastMessageText = lMsg.String
	return &c, nil
}

// ----------------- CRM MESSAGES -----------------

func (r *Repository) InsertMessage(m *Message) error {
	r.mgr.Mu.Lock()
	defer r.mgr.Mu.Unlock()

	now := time.Now().Unix()
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	if m.Timestamp == 0 {
		m.Timestamp = now
	}

	query := `
	INSERT OR IGNORE INTO crm_messages (id, conversation_id, external_msg_id, sender_type, message_type, content, media_url, status, timestamp, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := r.mgr.DB.Exec(query, m.ID, m.ConversationID, m.ExternalMsgID, m.SenderType, m.MessageType, m.Content, m.MediaURL, m.Status, m.Timestamp, m.CreatedAt)
	return err
}

func (r *Repository) GetMessages(convID string, limit int) ([]Message, error) {
	r.mgr.Mu.RLock()
	defer r.mgr.Mu.RUnlock()

	if limit <= 0 || limit > 200 {
		limit = 100
	}

	query := `SELECT id, conversation_id, external_msg_id, sender_type, message_type, content, media_url, status, timestamp, created_at FROM crm_messages WHERE conversation_id = ? ORDER BY timestamp ASC LIMIT ?`
	rows, err := r.mgr.DB.Query(query, convID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Message
	for rows.Next() {
		var m Message
		var extID, content, mUrl sql.NullString
		err := rows.Scan(&m.ID, &m.ConversationID, &extID, &m.SenderType, &m.MessageType, &content, &mUrl, &m.Status, &m.Timestamp, &m.CreatedAt)
		if err != nil {
			return nil, err
		}
		m.ExternalMsgID = extID.String
		m.Content = content.String
		m.MediaURL = mUrl.String
		list = append(list, m)
	}
	return list, nil
}

// ----------------- POST QUEUE -----------------

func (r *Repository) InsertPostQueue(item *PostQueueItem) error {
	r.mgr.Mu.Lock()
	defer r.mgr.Mu.Unlock()

	now := time.Now().Unix()
	if item.CreatedAt == 0 {
		item.CreatedAt = now
	}
	item.UpdatedAt = now

	query := `
	INSERT INTO post_queue (id, user_id, account_id, platform, post_type, caption, title, media_paths, scheduled_at, status, retry_count, error_message, published_post_id, published_post_url, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := r.mgr.DB.Exec(query, item.ID, item.UserID, item.AccountID, item.Platform, item.PostType, item.Caption, item.Title, item.MediaPaths, item.ScheduledAt, item.Status, item.RetryCount, item.ErrorMessage, item.PublishedPostID, item.PublishedPostURL, item.CreatedAt, item.UpdatedAt)
	return err
}

func (r *Repository) GetPostHistory(userID int64, status string) ([]PostQueueItem, error) {
	r.mgr.Mu.RLock()
	defer r.mgr.Mu.RUnlock()

	var query string
	var rows *sql.Rows
	var err error

	if status != "" && status != "all" {
		query = `SELECT id, user_id, account_id, platform, post_type, caption, title, media_paths, scheduled_at, status, retry_count, error_message, published_post_id, published_post_url, created_at, updated_at FROM post_queue WHERE user_id = ? AND status = ? ORDER BY scheduled_at DESC LIMIT 100`
		rows, err = r.mgr.DB.Query(query, userID, status)
	} else {
		query = `SELECT id, user_id, account_id, platform, post_type, caption, title, media_paths, scheduled_at, status, retry_count, error_message, published_post_id, published_post_url, created_at, updated_at FROM post_queue WHERE user_id = ? ORDER BY scheduled_at DESC LIMIT 100`
		rows, err = r.mgr.DB.Query(query, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []PostQueueItem
	for rows.Next() {
		var p PostQueueItem
		var cap, title, mPaths, eMsg, pubID, pubUrl sql.NullString
		err := rows.Scan(&p.ID, &p.UserID, &p.AccountID, &p.Platform, &p.PostType, &cap, &title, &mPaths, &p.ScheduledAt, &p.Status, &p.RetryCount, &eMsg, &pubID, &pubUrl, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		p.Caption = cap.String
		p.Title = title.String
		p.MediaPaths = mPaths.String
		p.ErrorMessage = eMsg.String
		p.PublishedPostID = pubID.String
		p.PublishedPostURL = pubUrl.String
		list = append(list, p)
	}
	return list, nil
}

func (r *Repository) GetPendingPosts(now int64, limit int) ([]PostQueueItem, error) {
	r.mgr.Mu.RLock()
	defer r.mgr.Mu.RUnlock()

	query := `SELECT id, user_id, account_id, platform, post_type, caption, title, media_paths, scheduled_at, status, retry_count, error_message, published_post_id, published_post_url, created_at, updated_at FROM post_queue WHERE status = 'pending' AND scheduled_at <= ? ORDER BY scheduled_at ASC LIMIT ?`
	rows, err := r.mgr.DB.Query(query, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []PostQueueItem
	for rows.Next() {
		var p PostQueueItem
		var cap, title, mPaths, eMsg, pubID, pubUrl sql.NullString
		err := rows.Scan(&p.ID, &p.UserID, &p.AccountID, &p.Platform, &p.PostType, &cap, &title, &mPaths, &p.ScheduledAt, &p.Status, &p.RetryCount, &eMsg, &pubID, &pubUrl, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		p.Caption = cap.String
		p.Title = title.String
		p.MediaPaths = mPaths.String
		p.ErrorMessage = eMsg.String
		p.PublishedPostID = pubID.String
		p.PublishedPostURL = pubUrl.String
		list = append(list, p)
	}
	return list, nil
}

func (r *Repository) UpdatePostStatus(id, status, errorMsg, pubID, pubURL string) error {
	r.mgr.Mu.Lock()
	defer r.mgr.Mu.Unlock()

	now := time.Now().Unix()
	query := `UPDATE post_queue SET status = ?, error_message = ?, published_post_id = ?, published_post_url = ?, updated_at = ? WHERE id = ?`
	_, err := r.mgr.DB.Exec(query, status, errorMsg, pubID, pubURL, now, id)
	return err
}

func (r *Repository) DeletePost(id string, userID int64) error {
	r.mgr.Mu.Lock()
	defer r.mgr.Mu.Unlock()

	var res sql.Result
	var err error
	if userID > 0 {
		res, err = r.mgr.DB.Exec(`DELETE FROM post_queue WHERE id = ? AND user_id = ? AND status IN ('pending', 'failed')`, id, userID)
	} else {
		res, err = r.mgr.DB.Exec(`DELETE FROM post_queue WHERE id = ? AND status IN ('pending', 'failed')`, id)
	}
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("postingan tidak ditemukan atau tidak dapat dibatalkan")
	}
	return nil
}
