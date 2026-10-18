package ws

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"restman/internal/model"
)

type session struct {
	conn     *websocket.Conn
	cancel   context.CancelFunc
	messages []model.WSMessage
	seq      int
}

type Hub struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func New() *Hub {
	return &Hub{sessions: map[string]*session{}}
}

func (h *Hub) Connect(tabID, url string, headers []model.KeyValue, protocols []string, pingEvery time.Duration) error {
	h.Close(tabID)
	header := http.Header{}
	for _, item := range headers {
		if item.Enabled && item.Key != "" {
			header.Add(item.Key, item.Value)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header, Subprotocols: protocols})
	if err != nil {
		cancel()
		return err
	}
	sess := &session{conn: conn, cancel: cancel}
	h.mu.Lock()
	h.sessions[tabID] = sess
	h.mu.Unlock()
	h.push(tabID, "system", "connected", false)
	if pingEvery > 0 {
		go h.ping(tabID, ctx, conn, pingEvery)
	}
	go h.read(tabID, ctx, conn)
	return nil
}

func (h *Hub) ping(tabID string, ctx context.Context, conn *websocket.Conn, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				h.push(tabID, "system", "ping failed: "+err.Error(), false)
				return
			}
			h.push(tabID, "system", "ping", false)
		}
	}
}

func (h *Hub) Send(tabID, data string, binary bool) error {
	h.mu.Lock()
	sess := h.sessions[tabID]
	h.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("socket is not connected")
	}
	msgType := websocket.MessageText
	payload := []byte(data)
	if binary {
		msgType = websocket.MessageBinary
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return fmt.Errorf("binary payload must be base64: %w", err)
		}
		payload = decoded
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sess.conn.Write(ctx, msgType, payload); err != nil {
		return err
	}
	h.push(tabID, "out", data, binary)
	return nil
}

func (h *Hub) Close(tabID string) {
	h.mu.Lock()
	sess := h.sessions[tabID]
	delete(h.sessions, tabID)
	h.mu.Unlock()
	if sess != nil {
		sess.cancel()
		_ = sess.conn.Close(websocket.StatusNormalClosure, "closed")
	}
}

func (h *Hub) Poll(tabID string, after int) []model.WSMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	sess := h.sessions[tabID]
	if sess == nil {
		return []model.WSMessage{}
	}
	out := []model.WSMessage{}
	for _, msg := range sess.messages {
		if msg.Seq > after {
			out = append(out, msg)
		}
	}
	return out
}

func (h *Hub) read(tabID string, ctx context.Context, conn *websocket.Conn) {
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			h.push(tabID, "system", "disconnected", false)
			return
		}
		binary := msgType == websocket.MessageBinary
		text := string(data)
		if binary {
			text = base64.StdEncoding.EncodeToString(data)
		}
		h.push(tabID, "in", text, binary)
	}
}

func (h *Hub) push(tabID, direction, data string, binary bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sess := h.sessions[tabID]
	if sess == nil {
		return
	}
	sess.seq++
	sess.messages = append(sess.messages, model.WSMessage{
		Seq: sess.seq, Direction: direction, Data: data, Binary: binary,
		Time: time.Now().UTC().Format(time.RFC3339),
	})
	if len(sess.messages) > 500 {
		sess.messages = sess.messages[len(sess.messages)-500:]
	}
}
