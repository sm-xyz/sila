package crm

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ClientConn struct {
	UserID int64
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

type Hub struct {
	clients map[int64]map[*ClientConn]bool
	mu      sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[int64]map[*ClientConn]bool),
	}
}

// Upgrade upgrades HTTP connection to RFC 6455 WebSocket with zero external dependencies
func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request, userID int64) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "Expected WebSocket Upgrade", http.StatusBadRequest)
		return
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "Missing Sec-WebSocket-Key", http.StatusBadRequest)
		return
	}

	// Compute accept hash
	hash := sha1.New()
	hash.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(hash.Sum(nil))

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Webserver does not support hijacking", http.StatusInternalServerError)
		return
	}

	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		log.Printf("[SILA Hub] Hijack failed: %v", err)
		return
	}

	// Send handshake response
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	conn.Write([]byte(response))

	client := &ClientConn{
		UserID: userID,
		conn:   conn,
		reader: bufrw.Reader,
	}

	h.register(client)
	defer h.unregister(client)

	// Keep alive & read loop
	for {
		// Read WebSocket frame header
		b1, err := client.reader.ReadByte()
		if err != nil {
			break
		}
		opcode := b1 & 0x0F
		if opcode == 0x8 { // Close frame
			break
		}
		if opcode == 0x9 { // Ping
			client.sendPong()
			continue
		}

		b2, err := client.reader.ReadByte()
		if err != nil {
			break
		}
		isMasked := (b2 & 0x80) != 0
		payloadLen := int(b2 & 0x7F)

		if payloadLen == 126 {
			b := make([]byte, 2)
			if _, err := io.ReadFull(client.reader, b); err != nil {
				break
			}
			payloadLen = int(b[0])<<8 | int(b[1])
		} else if payloadLen == 127 {
			b := make([]byte, 8)
			if _, err := io.ReadFull(client.reader, b); err != nil {
				break
			}
			payloadLen = int(b[4])<<24 | int(b[5])<<16 | int(b[6])<<8 | int(b[7])
		}

		var maskKey [4]byte
		if isMasked {
			if _, err := io.ReadFull(client.reader, maskKey[:]); err != nil {
				break
			}
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(client.reader, payload); err != nil {
			break
		}
		if isMasked {
			for i := 0; i < payloadLen; i++ {
				payload[i] ^= maskKey[i%4]
			}
		}
	}
}

func (h *Hub) register(c *ClientConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.UserID] == nil {
		h.clients[c.UserID] = make(map[*ClientConn]bool)
	}
	h.clients[c.UserID][c] = true
	log.Printf("[SILA Hub] Client connected (User %d). Total active clients for user: %d", c.UserID, len(h.clients[c.UserID]))
}

func (h *Hub) unregister(c *ClientConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if userClients, ok := h.clients[c.UserID]; ok {
		delete(userClients, c)
		if len(userClients) == 0 {
			delete(h.clients, c.UserID)
		}
	}
	c.conn.Close()
	log.Printf("[SILA Hub] Client disconnected (User %d)", c.UserID)
}

func (h *Hub) BroadcastToUser(userID int64, data interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	userClients, ok := h.clients[userID]
	if !ok || len(userClients) == 0 {
		return
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return
	}

	for c := range userClients {
		go c.sendTextMessage(payload)
	}
}

func (h *Hub) BroadcastAll(data interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	payload, err := json.Marshal(data)
	if err != nil {
		return
	}

	for _, userClients := range h.clients {
		for c := range userClients {
			go c.sendTextMessage(payload)
		}
	}
}

func (c *ClientConn) sendTextMessage(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	length := len(data)
	var header []byte

	if length <= 125 {
		header = []byte{0x81, byte(length)}
	} else if length <= 65535 {
		header = []byte{0x81, 126, byte(length >> 8), byte(length & 0xFF)}
	} else {
		header = []byte{0x81, 127, 0, 0, 0, 0, byte(length >> 24), byte((length >> 16) & 0xFF), byte((length >> 8) & 0xFF), byte(length & 0xFF)}
	}

	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(data)
	return err
}

func (c *ClientConn) sendPong() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.Write([]byte{0x8A, 0x00})
}
