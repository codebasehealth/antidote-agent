package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/codebasehealth/antidote-agent/internal/messages"
	"github.com/gorilla/websocket"
)

const (
	// Version is the agent version
	Version = "2.0.0"

	// Connection states
	StateDisconnected = "disconnected"
	StateConnecting   = "connecting"
	StateConnected    = "connected"

	// Reconnection settings
	InitialDelay = 1 * time.Second
	MaxDelay     = 30 * time.Second
	Multiplier   = 2.0

	// Heartbeat interval
	HeartbeatInterval = 30 * time.Second
)

// MessageHandler is called when a message is received
type MessageHandler func(msgType string, data []byte)

// Manager manages the WebSocket connection to the server
type Manager struct {
	token    string
	endpoint string
	conn     *websocket.Conn
	state    string
	serverID string
	handler  MessageHandler

	sendCh chan []byte
	doneCh chan struct{}
	mu     sync.RWMutex
	wg     sync.WaitGroup
}

// NewManager creates a new connection manager
func NewManager(token, endpoint string, handler MessageHandler) *Manager {
	return &Manager{
		token:    token,
		endpoint: endpoint,
		state:    StateDisconnected,
		handler:  handler,
		sendCh:   make(chan []byte, 100),
		doneCh:   make(chan struct{}),
	}
}

// Start begins the connection manager
func (m *Manager) Start(ctx context.Context) error {
	m.wg.Add(1)
	go m.connectionLoop(ctx)
	return nil
}

// Stop gracefully stops the connection manager
func (m *Manager) Stop() {
	close(m.doneCh)
	m.wg.Wait()

	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
	}
	m.mu.Unlock()
}

// Send queues a message to be sent
func (m *Manager) Send(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	select {
	case m.sendCh <- data:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// State returns the current connection state
func (m *Manager) State() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// ServerID returns the server ID assigned by the server
func (m *Manager) ServerID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.serverID
}

// connectionLoop manages the connection lifecycle
func (m *Manager) connectionLoop(ctx context.Context) {
	defer m.wg.Done()

	delay := InitialDelay

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.doneCh:
			return
		default:
		}

		// Attempt to connect
		m.setState(StateConnecting)
		err := m.connect(ctx)

		if err != nil {
			log.Printf("Connection failed: %v", err)
			m.setState(StateDisconnected)

			// Wait before reconnecting
			select {
			case <-ctx.Done():
				return
			case <-m.doneCh:
				return
			case <-time.After(delay):
			}

			// Exponential backoff
			delay = time.Duration(float64(delay) * Multiplier)
			if delay > MaxDelay {
				delay = MaxDelay
			}
			continue
		}

		// Reset delay on successful connection
		delay = InitialDelay

		// Run the connection
		m.runConnection(ctx)
		m.setState(StateDisconnected)
	}
}

// connect establishes a WebSocket connection and authenticates
func (m *Manager) connect(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	log.Printf("Connecting to %s...", m.endpoint)

	conn, _, err := dialer.DialContext(ctx, m.endpoint, http.Header{})
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	m.mu.Lock()
	m.conn = conn
	m.mu.Unlock()

	// Send auth message
	hostname, _ := os.Hostname()
	authMsg := messages.NewAuthMessage(
		m.token,
		Version,
		hostname,
		runtime.GOOS,
		runtime.GOARCH,
	)

	if err := m.sendMessage(authMsg); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send auth: %w", err)
	}

	// Wait for auth response
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to read auth response: %w", err)
	}
	conn.SetReadDeadline(time.Time{})

	// Debug: log the raw response
	log.Printf("Auth response received: messageType=%d, len=%d, data=%s", messageType, len(data), string(data))

	msgType, err := messages.ParseMessage(data)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to parse auth response: %w", err)
	}

	log.Printf("Parsed message type: %s", msgType)

	if msgType == messages.TypeAuthError {
		var authErr messages.AuthErrorMessage
		json.Unmarshal(data, &authErr)
		conn.Close()
		return fmt.Errorf("auth failed: %s", authErr.Message)
	}

	if msgType != messages.TypeAuthOK {
		conn.Close()
		return fmt.Errorf("unexpected response: %s", msgType)
	}

	var authOK messages.AuthOKMessage
	json.Unmarshal(data, &authOK)

	m.mu.Lock()
	m.serverID = authOK.ServerID
	m.mu.Unlock()

	m.setState(StateConnected)
	log.Printf("Connected! Server ID: %s", authOK.ServerID)

	return nil
}

// runConnection handles the connection after authentication
func (m *Manager) runConnection(ctx context.Context) {
	// Start heartbeat
	heartbeatTicker := time.NewTicker(HeartbeatInterval)
	defer heartbeatTicker.Stop()

	// Start read goroutine
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		m.readLoop()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.doneCh:
			return
		case <-readDone:
			return
		case <-heartbeatTicker.C:
			if err := m.sendMessage(messages.NewHeartbeatMessage()); err != nil {
				log.Printf("Failed to send heartbeat: %v", err)
				return
			}
		case data := <-m.sendCh:
			m.mu.RLock()
			conn := m.conn
			m.mu.RUnlock()

			if conn == nil {
				continue
			}

			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("Failed to send message: %v", err)
				return
			}
		}
	}
}

// readLoop reads messages from the WebSocket
func (m *Manager) readLoop() {
	for {
		m.mu.RLock()
		conn := m.conn
		m.mu.RUnlock()

		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Println("Connection closed normally")
			} else {
				log.Printf("Read error: %v", err)
			}
			return
		}

		msgType, err := messages.ParseMessage(data)
		if err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		if m.handler != nil {
			m.handler(msgType, data)
		}
	}
}

// sendMessage marshals and sends a message
func (m *Manager) sendMessage(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	m.mu.RLock()
	conn := m.conn
	m.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}

// setState updates the connection state
func (m *Manager) setState(state string) {
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
}
