// internal/websocket/ws.go
package websocket

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"lsengine/internal/metrics"
	"lsengine/internal/vm"
)

const (
	MAX_WEBSOCKET_MESSAGE = 5 * 1024 * 1024
	WS_WRITE_WAIT         = 10 * time.Second
	WS_PONG_WAIT          = 60 * time.Second
	WS_PING_PERIOD        = 30 * time.Second
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	ReadBufferSize:    4096,
	WriteBufferSize:   4096,
	EnableCompression: true,
	HandshakeTimeout:  10 * time.Second,
}

type WSConnection struct {
	conn         *websocket.Conn
	send         chan []byte
	createdAt    time.Time
	lastActivity time.Time
	mu           sync.Mutex
	closed       bool
	id           string
}

type WSManager struct {
	connections sync.Map
	maxConns    int
	active      int64
	total       int64
}

func NewWSManager(maxConns int) *WSManager {
	return &WSManager{
		maxConns: maxConns,
	}
}

func (m *WSManager) Register(ws *WSConnection) bool {
	count := 0
	m.connections.Range(func(_, _ interface{}) bool {
		count++
		return true
	})

	if count >= m.maxConns {
		return false
	}

	m.connections.Store(ws, true)
	atomic.AddInt64(&m.active, 1)
	atomic.AddInt64(&m.total, 1)
	metrics.GlobalMetrics.IncActiveConnections()
	return true
}

func (m *WSManager) Unregister(ws *WSConnection) {
	m.connections.Delete(ws)
	atomic.AddInt64(&m.active, -1)
	metrics.GlobalMetrics.DecActiveConnections()
}

func (m *WSManager) Broadcast(message []byte) {
	m.connections.Range(func(key, _ interface{}) bool {
		conn := key.(*WSConnection)
		select {
		case conn.send <- message:
		default:
			atomic.AddInt64(&metrics.GlobalMetrics.WsDropped, 1)
		}
		return true
	})
}

func (m *WSManager) GetStats() map[string]interface{} {
	count := 0
	m.connections.Range(func(_, _ interface{}) bool {
		count++
		return true
	})

	return map[string]interface{}{
		"active":  atomic.LoadInt64(&m.active),
		"total":   atomic.LoadInt64(&m.total),
		"max":     m.maxConns,
		"current": count,
	}
}

func (m *WSManager) HandleWebSocket(w http.ResponseWriter, r *http.Request, projectRoot string) {
	count := 0
	m.connections.Range(func(_, _ interface{}) bool {
		count++
		return true
	})

	if count >= m.maxConns {
		http.Error(w, "Too many connections", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Error en upgrade: %v", err)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(MAX_WEBSOCKET_MESSAGE)
	conn.SetReadDeadline(time.Now().Add(WS_PONG_WAIT))

	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(WS_PONG_WAIT))
		return nil
	})

	wsConn := &WSConnection{
		conn:         conn,
		send:         make(chan []byte, 256),
		createdAt:    time.Now(),
		lastActivity: time.Now(),
		closed:       false,
		id:           fmt.Sprintf("conn_%d_%d", time.Now().UnixNano(), time.Now().UnixNano()%10000),
	}

	if !m.Register(wsConn) {
		conn.Close()
		return
	}
	defer m.Unregister(wsConn)

	welcomeMsg := fmt.Sprintf("Conectado al servidor WebSocket - ID: %s", wsConn.id)
	err = conn.WriteMessage(websocket.TextMessage, []byte(welcomeMsg))
	if err != nil {
		log.Printf("Error sending welcome message: %v", err)
		return
	}

	go m.wsWriter(wsConn)

	defer func() {
		wsConn.mu.Lock()
		wsConn.closed = true
		wsConn.mu.Unlock()
		close(wsConn.send)
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Error reading message: %v", err)
			}
			break
		}

		atomic.AddInt64(&metrics.GlobalMetrics.WsMessagesReceived, 1)
		wsConn.lastActivity = time.Now()

		go m.handleMessage(wsConn, msg, projectRoot)
	}
}

func (m *WSManager) wsWriter(ws *WSConnection) {
	ticker := time.NewTicker(WS_PING_PERIOD)
	defer ticker.Stop()
	defer ws.conn.Close()

	for {
		select {
		case message, ok := <-ws.send:
			ws.conn.SetWriteDeadline(time.Now().Add(WS_WRITE_WAIT))
			if !ok {
				ws.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := ws.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			n := len(ws.send)
			for i := 0; i < n && i < 10; i++ {
				w.Write([]byte{'\n'})
				select {
				case msg := <-ws.send:
					w.Write(msg)
				default:
					break
				}
			}

			if err := w.Close(); err != nil {
				return
			}

			atomic.AddInt64(&metrics.GlobalMetrics.WsMessagesSent, 1)

		case <-ticker.C:
			ws.conn.SetWriteDeadline(time.Now().Add(WS_WRITE_WAIT))
			if err := ws.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (m *WSManager) handleMessage(ws *WSConnection, msg []byte, projectRoot string) {
	code := string(msg)

	if len(code) > MAX_WEBSOCKET_MESSAGE {
		ws.safeSend([]byte("Error: Code too long"))
		return
	}

	output, err := vm.GlobalGojaPool.Execute(code, projectRoot)
	if err != nil {
		ws.safeSend([]byte(fmt.Sprintf("Error: %v", err)))
		return
	}

	ws.safeSend([]byte(output))
}

func (ws *WSConnection) safeSend(msg []byte) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.closed {
		return
	}

	select {
	case ws.send <- msg:
	default:
		atomic.AddInt64(&metrics.GlobalMetrics.WsDropped, 1)
	}
}