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

	"github.com/codebasehealth/antidote-agent/internal/config"
	"github.com/codebasehealth/antidote-agent/internal/messages"
	"github.com/gorilla/websocket"
)

const (
	// Version is the agent version
	Version = "1.0.0"

	// StateDisconnected means the agent is not connected
	StateDisconnected = "disconnected"
	// StateConnecting means the agent is attempting to connect
	StateConnecting = "connecting"
	// StateConnected means the agent is connected and authenticated
	StateConnected = "connected"
)

// MessageHandler is called when a message is received
type MessageHandler func(msgType string, data []byte)

// Manager manages the WebSocket connection to the server
type Manager struct {
	cfg      *config.Config
	conn     *websocket.Conn
	state    string
	serverID string
	handler  MessageHandler

	sendCh   chan []byte
	doneCh   chan struct{}
	mu       sync.RWMutex
	wg       sync.WaitGroup
}

// NewManager creates a new connection manager
func NewManager(cfg *config.Config, handler MessageHandler) *Manager {
	return &Manager{
		cfg:     cfg,
		state:   StateDisconnected,
		handler: handler,
		sendCh:  make(chan []byte, 100),
		doneCh:  make(chan struct{}),
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

	delay := m.cfg.Connection.Reconnect.InitialDelay

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
			delay = time.Duration(float64(delay) * m.cfg.Connection.Reconnect.Multiplier)
			if delay > m.cfg.Connection.Reconnect.MaxDelay {
				delay = m.cfg.Connection.Reconnect.MaxDelay
			}
			continue
		}

		// Reset delay on successful connection
		delay = m.cfg.Connection.Reconnect.InitialDelay

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

	log.Printf("Connecting to %s...", m.cfg.Connection.Endpoint)

	conn, _, err := dialer.DialContext(ctx, m.cfg.Connection.Endpoint, http.Header{})
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	m.mu.Lock()
	m.conn = conn
	m.mu.Unlock()

	// Send auth message
	hostname, _ := os.Hostname()
	authMsg := messages.NewAuthMessage(
		m.cfg.Connection.Token,
		Version,
		messages.ServerInfo{
			Name:        m.cfg.Server.Name,
			Environment: m.cfg.Server.Environment,
			Hostname:    hostname,
			OS:          runtime.GOOS,
			Arch:        runtime.GOARCH,
		},
		[]string{"logs"},
		m.cfg.GetActionNames(),
	)

	if err := m.sendMessage(authMsg); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send auth: %w", err)
	}

	// Wait for auth response
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to read auth response: %w", err)
	}
	conn.SetReadDeadline(time.Time{})

	authResp, err := messages.ParseAuthResponseMessage(data)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to parse auth response: %w", err)
	}

	if authResp.Status != "ok" {
		conn.Close()
		return fmt.Errorf("auth failed: %s", authResp.Error)
	}

	m.mu.Lock()
	m.serverID = authResp.ServerID
	m.mu.Unlock()

	m.setState(StateConnected)
	log.Printf("Connected! Server ID: %s", authResp.ServerID)

	return nil
}

// runConnection handles the connection after authentication
func (m *Manager) runConnection(ctx context.Context) {
	// Start heartbeat
	heartbeatTicker := time.NewTicker(m.cfg.Connection.Heartbeat)
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
