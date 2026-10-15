package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

type DBManager struct {
	DB *sql.DB
	Mu sync.RWMutex
}

func InitDB(dbPath string) (*DBManager, error) {
	if dbPath == "" {
		dbPath = "./data/sila.db"
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("gagal membuat direktori database %s: %w", dir, err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("gagal membuka koneksi sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	mgr := &DBManager{DB: db}
	if err := mgr.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("gagal menjalankan migrasi schema: %w", err)
	}

	log.Printf("[SILA DB] SQLite database berhasil diinisialisasi (WAL Mode): %s", dbPath)
	return mgr, nil
}

func (m *DBManager) Close() error {
	if m.DB != nil {
		return m.DB.Close()
	}
	return nil
}

func (m *DBManager) migrate() error {
	schema := `
	PRAGMA journal_mode=WAL;

	CREATE TABLE IF NOT EXISTS accounts (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		platform TEXT NOT NULL,
		account_id TEXT NOT NULL,
		account_name TEXT NOT NULL,
		account_username TEXT,
		avatar_url TEXT,
		access_token TEXT NOT NULL,
		refresh_token TEXT,
		token_expires_at INTEGER,
		extra_metadata TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		UNIQUE(user_id, platform, account_id)
	);

	CREATE TABLE IF NOT EXISTS crm_conversations (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL,
		platform TEXT NOT NULL,
		external_conv_id TEXT NOT NULL,
		customer_id TEXT NOT NULL,
		customer_name TEXT NOT NULL,
		customer_avatar TEXT,
		last_message_text TEXT,
		last_message_time INTEGER NOT NULL,
		unread_count INTEGER DEFAULT 0,
		is_starred INTEGER DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE CASCADE,
		UNIQUE(account_id, external_conv_id)
	);

	CREATE TABLE IF NOT EXISTS crm_messages (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		external_msg_id TEXT UNIQUE,
		sender_type TEXT NOT NULL,
		message_type TEXT DEFAULT 'text',
		content TEXT,
		media_url TEXT,
		status TEXT DEFAULT 'received',
		timestamp INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		FOREIGN KEY(conversation_id) REFERENCES crm_conversations(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS post_queue (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		account_id TEXT NOT NULL,
		platform TEXT NOT NULL,
		post_type TEXT NOT NULL,
		caption TEXT,
		title TEXT,
		media_paths TEXT,
		scheduled_at INTEGER NOT NULL,
		status TEXT DEFAULT 'pending',
		retry_count INTEGER DEFAULT 0,
		error_message TEXT,
		published_post_id TEXT,
		published_post_url TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_conv_account ON crm_conversations(account_id, last_message_time DESC);
	CREATE INDEX IF NOT EXISTS idx_msg_conv ON crm_messages(conversation_id, timestamp ASC);
	CREATE INDEX IF NOT EXISTS idx_post_queue_sched ON post_queue(status, scheduled_at ASC);
	CREATE INDEX IF NOT EXISTS idx_accounts_user ON accounts(user_id, platform);
	`

	_, err := m.DB.Exec(schema)
	return err
}
