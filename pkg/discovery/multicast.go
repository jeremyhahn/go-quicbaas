//go:build raft

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package discovery provides cluster node discovery mechanisms for QuicBaaS.
package discovery

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultMulticastGroup is the default IPv4 multicast address for QuicBaaS discovery.
	// Uses the local network control block (224.0.0.0/24 is link-local).
	// 239.x.x.x is the administratively scoped range for site-local usage.
	DefaultMulticastGroup = "239.255.77.77"

	// DefaultMulticastPort is the default UDP port for multicast discovery.
	DefaultMulticastPort = 63777

	// DefaultBroadcastInterval is how often to send announcements.
	DefaultBroadcastInterval = 2 * time.Second

	// DefaultPeerTTL is how long to keep a peer in the list without hearing from it.
	DefaultPeerTTL = 10 * time.Second

	// maxMessageSize is the maximum size of a discovery message.
	maxMessageSize = 4096

	// magicNumber identifies QuicBaaS discovery messages.
	magicNumber uint32 = 0x44524147 // "DRAG"

	// protocolVersion for forward compatibility.
	protocolVersion uint8 = 1
)

// MessageType identifies the type of discovery message.
type MessageType uint8

const (
	// MessageAnnounce is sent periodically to advertise presence.
	MessageAnnounce MessageType = 1

	// MessageQuery is sent to request immediate responses from all nodes.
	MessageQuery MessageType = 2

	// MessageResponse is sent in reply to a query.
	MessageResponse MessageType = 3

	// MessageLeave is sent when a node gracefully departs.
	MessageLeave MessageType = 4
)

// DiscoveryMessage is the wire format for multicast discovery.
type DiscoveryMessage struct {
	Magic     uint32            `json:"-"` // Set in header, not JSON
	Version   uint8             `json:"-"` // Set in header, not JSON
	Type      MessageType       `json:"type"`
	NodeID    uint64            `json:"node_id"`
	Address   string            `json:"address"`
	RaftAddr  string            `json:"raft_addr"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp int64             `json:"timestamp"`
}

// MulticastConfig configures the multicast discovery method.
type MulticastConfig struct {
	// MulticastGroup is the multicast IP address to join.
	// Default: 239.255.77.77
	MulticastGroup string

	// Port is the UDP port to use.
	// Default: 63777
	Port int

	// Interface is the network interface to use for multicast.
	// If empty, the system default is used.
	Interface string

	// BroadcastInterval is how often to send announcements.
	// Default: 2s
	BroadcastInterval time.Duration

	// PeerTTL is how long to remember a peer without hearing from it.
	// Default: 10s
	PeerTTL time.Duration

	// SendQueryOnStart sends a query message on startup for faster discovery.
	// Default: true
	SendQueryOnStart bool

	// ClusterSecret is a shared secret for authenticating discovery messages.
	// If set, all messages are signed with HMAC-SHA256 and verified on receipt.
	// If empty, discovery is unauthenticated (open cluster).
	ClusterSecret string
}

// DefaultMulticastConfig returns a MulticastConfig with sensible defaults.
func DefaultMulticastConfig() *MulticastConfig {
	return &MulticastConfig{
		MulticastGroup:    DefaultMulticastGroup,
		Port:              DefaultMulticastPort,
		BroadcastInterval: DefaultBroadcastInterval,
		PeerTTL:           DefaultPeerTTL,
		SendQueryOnStart:  true,
	}
}

// MulticastDiscovery implements discovery using UDP multicast.
// This enables zero-configuration peer discovery on LANs.
type MulticastDiscovery struct {
	config *MulticastConfig
	self   atomic.Value // stores *Peer

	// peers is the map of discovered peers by NodeID
	peers   map[uint64]*peerEntry
	peersMu sync.RWMutex

	// conn is the multicast UDP connection
	conn   *net.UDPConn
	connMu sync.RWMutex

	// closeChan signals shutdown
	closeChan chan struct{}
	closeOnce sync.Once
	running   atomic.Bool
}

// peerEntry stores a peer with its last seen time.
type peerEntry struct {
	peer     Peer
	lastSeen time.Time
}

// MulticastMessageTooShortError is returned when a multicast message is too short.
type MulticastMessageTooShortError struct{}

// Error implements the error interface.
func (e *MulticastMessageTooShortError) Error() string {
	return "message too short"
}

// MulticastInvalidMagicError is returned when a multicast message has an invalid magic number.
type MulticastInvalidMagicError struct{}

// Error implements the error interface.
func (e *MulticastInvalidMagicError) Error() string {
	return "invalid magic number"
}

// MulticastUnsupportedVersionError is returned when a multicast message has an unsupported protocol version.
type MulticastUnsupportedVersionError struct {
	Version uint8
}

// Error implements the error interface.
func (e *MulticastUnsupportedVersionError) Error() string {
	return "unsupported protocol version: " + strconv.Itoa(int(e.Version))
}

// MulticastAuthFormatError is returned when a multicast authenticated message has an invalid format.
type MulticastAuthFormatError struct {
	Detail string
}

// Error implements the error interface.
func (e *MulticastAuthFormatError) Error() string {
	return "authenticated message error: " + e.Detail
}

// MulticastInvalidSignatureError is returned when a multicast message signature is invalid.
type MulticastInvalidSignatureError struct{}

// Error implements the error interface.
func (e *MulticastInvalidSignatureError) Error() string {
	return "invalid message signature"
}

// NewMulticastDiscovery creates a new multicast discovery method.
func NewMulticastDiscovery(config *MulticastConfig) (*MulticastDiscovery, error) {
	if config == nil {
		config = DefaultMulticastConfig()
	}

	// Validate config
	if config.MulticastGroup == "" {
		config.MulticastGroup = DefaultMulticastGroup
	}

	if config.Port == 0 {
		config.Port = DefaultMulticastPort
	}

	if config.BroadcastInterval == 0 {
		config.BroadcastInterval = DefaultBroadcastInterval
	}

	if config.PeerTTL == 0 {
		config.PeerTTL = DefaultPeerTTL
	}

	// Validate multicast address
	ip := net.ParseIP(config.MulticastGroup)
	if ip == nil {
		return nil, &DiscoveryError{
			Op:     "NewMulticastDiscovery",
			Method: "multicast",
			Err:    "invalid multicast group: " + config.MulticastGroup,
		}
	}

	if !ip.IsMulticast() {
		return nil, &DiscoveryError{
			Op:     "NewMulticastDiscovery",
			Method: "multicast",
			Err:    "address " + config.MulticastGroup + " is not a multicast address",
		}
	}

	return &MulticastDiscovery{
		config:    config,
		peers:     make(map[uint64]*peerEntry),
		closeChan: make(chan struct{}),
	}, nil
}

// Name returns the name of this discovery method.
func (m *MulticastDiscovery) Name() string {
	return "multicast"
}

// Start begins listening for multicast messages and broadcasting announcements.
// This should be called after Register.
func (m *MulticastDiscovery) Start(ctx context.Context) error {
	if m.running.Load() {
		return nil
	}

	// Get the network interface if specified
	var ifi *net.Interface
	if m.config.Interface != "" {
		var err error
		ifi, err = net.InterfaceByName(m.config.Interface)
		if err != nil {
			return &DiscoveryError{
				Op:     "Start",
				Method: "multicast",
				Err:    "interface " + m.config.Interface + " not found: " + err.Error(),
			}
		}
	}

	// Resolve multicast address
	addr, err := net.ResolveUDPAddr("udp4", m.config.MulticastGroup+":"+strconv.Itoa(m.config.Port))
	if err != nil {
		return &DiscoveryError{
			Op:     "Start",
			Method: "multicast",
			Err:    "failed to resolve multicast address: " + err.Error(),
		}
	}

	// Join multicast group
	conn, err := net.ListenMulticastUDP("udp4", ifi, addr)
	if err != nil {
		return &DiscoveryError{
			Op:     "Start",
			Method: "multicast",
			Err:    "failed to join multicast group: " + err.Error(),
		}
	}

	// Set read buffer size
	if err := conn.SetReadBuffer(65536); err != nil {
		_ = conn.Close() // Cleanup on error, ignore close error
		return &DiscoveryError{
			Op:     "Start",
			Method: "multicast",
			Err:    "failed to set read buffer: " + err.Error(),
		}
	}

	m.connMu.Lock()
	m.conn = conn
	m.connMu.Unlock()

	m.running.Store(true)

	// Start listener goroutine
	go m.listen()

	// Start broadcaster goroutine
	go m.broadcast()

	// Start cleanup goroutine
	go m.cleanup()

	// Send initial query if configured
	if m.config.SendQueryOnStart {
		m.sendQuery()
	}

	return nil
}

// Discover returns the currently known peers.
func (m *MulticastDiscovery) Discover(ctx context.Context) ([]Peer, error) {
	// If not running, start the discovery process
	if !m.running.Load() {
		if err := m.Start(ctx); err != nil {
			return nil, err
		}

		// Wait a bit for initial discovery
		select {
		case <-time.After(m.config.BroadcastInterval * 2):
		case <-ctx.Done():
			return nil, &DiscoveryError{
				Op:     "Discover",
				Method: "multicast",
				Err:    ctx.Err().Error(),
			}
		}
	}

	// Send a query to accelerate discovery
	m.sendQuery()

	// Give peers time to respond
	select {
	case <-time.After(500 * time.Millisecond):
	case <-ctx.Done():
	}

	return m.getPeers(), nil
}

// Register sets this node's information and begins advertising.
func (m *MulticastDiscovery) Register(ctx context.Context, self Peer) error {
	if self.NodeID == 0 {
		return &DiscoveryError{
			Op:     "Register",
			Method: "multicast",
			Err:    "NodeID cannot be 0",
		}
	}

	if self.Address == "" {
		return &DiscoveryError{
			Op:     "Register",
			Method: "multicast",
			Err:    "Address cannot be empty",
		}
	}

	// Ensure RaftAddr is set
	if self.RaftAddr == "" {
		self.RaftAddr = self.Address
	}

	m.self.Store(&self)

	// Start if not already running
	if !m.running.Load() {
		return m.Start(ctx)
	}

	// Send immediate announcement
	m.sendAnnounce()

	return nil
}

// Deregister announces departure and stops broadcasting.
func (m *MulticastDiscovery) Deregister(ctx context.Context) error {
	if !m.running.Load() {
		return nil
	}

	// Send leave message
	m.sendLeave()

	return nil
}

// Close stops the multicast listener and releases resources.
func (m *MulticastDiscovery) Close() error {
	m.closeOnce.Do(func() {
		// Send leave message before closing
		if m.running.Load() {
			m.sendLeave()
		}

		close(m.closeChan)
		m.running.Store(false)

		m.connMu.Lock()
		if m.conn != nil {
			_ = m.conn.Close() // Cleanup during stop, ignore close error
			m.conn = nil
		}
		m.connMu.Unlock()
	})

	return nil
}

// listen receives multicast messages and updates the peer list.
func (m *MulticastDiscovery) listen() {
	buf := make([]byte, maxMessageSize)

	for {
		select {
		case <-m.closeChan:
			return
		default:
		}

		m.connMu.RLock()
		conn := m.conn
		m.connMu.RUnlock()

		if conn == nil {
			return
		}

		// Set read deadline to allow checking closeChan
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second)) // Best effort deadline, ignore error

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			// Connection closed
			return
		}

		if n < 5 { // Minimum: magic(4) + version(1)
			continue
		}

		// Parse message
		msg, err := m.parseMessage(buf[:n])
		if err != nil {
			// Skip invalid messages (wrong PSK, malformed, etc.)
			continue
		}

		// Get our self info to skip our own messages
		selfPtr := m.self.Load()
		if selfPtr != nil {
			if self, ok := selfPtr.(*Peer); ok && msg.NodeID == self.NodeID {
				continue
			}
		}

		// Handle message
		m.handleMessage(msg)
	}
}

// broadcast sends periodic announcements.
func (m *MulticastDiscovery) broadcast() {
	ticker := time.NewTicker(m.config.BroadcastInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.closeChan:
			return
		case <-ticker.C:
			m.sendAnnounce()
		}
	}
}

// cleanup removes expired peers.
func (m *MulticastDiscovery) cleanup() {
	ticker := time.NewTicker(m.config.PeerTTL / 2)
	defer ticker.Stop()

	for {
		select {
		case <-m.closeChan:
			return
		case <-ticker.C:
			m.removeExpiredPeers()
		}
	}
}

// sendAnnounce broadcasts an announcement message.
func (m *MulticastDiscovery) sendAnnounce() {
	selfPtr := m.self.Load()
	if selfPtr == nil {
		return
	}
	self, ok := selfPtr.(*Peer)
	if !ok {
		return
	}

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    self.NodeID,
		Address:   self.Address,
		RaftAddr:  self.RaftAddr,
		Metadata:  self.Metadata,
		Timestamp: time.Now().UnixNano(),
	}

	m.sendMessage(msg)
}

// sendQuery broadcasts a query message to request responses.
func (m *MulticastDiscovery) sendQuery() {
	selfPtr := m.self.Load()
	if selfPtr == nil {
		return
	}
	self, ok := selfPtr.(*Peer)
	if !ok {
		return
	}

	msg := &DiscoveryMessage{
		Type:      MessageQuery,
		NodeID:    self.NodeID,
		Address:   self.Address,
		RaftAddr:  self.RaftAddr,
		Timestamp: time.Now().UnixNano(),
	}

	m.sendMessage(msg)
}

// sendLeave broadcasts a leave message.
func (m *MulticastDiscovery) sendLeave() {
	selfPtr := m.self.Load()
	if selfPtr == nil {
		return
	}
	self, ok := selfPtr.(*Peer)
	if !ok {
		return
	}

	msg := &DiscoveryMessage{
		Type:      MessageLeave,
		NodeID:    self.NodeID,
		Timestamp: time.Now().UnixNano(),
	}

	m.sendMessage(msg)
}

// sendResponse sends a response to a query.
func (m *MulticastDiscovery) sendResponse() {
	selfPtr := m.self.Load()
	if selfPtr == nil {
		return
	}
	self, ok := selfPtr.(*Peer)
	if !ok {
		return
	}

	msg := &DiscoveryMessage{
		Type:      MessageResponse,
		NodeID:    self.NodeID,
		Address:   self.Address,
		RaftAddr:  self.RaftAddr,
		Metadata:  self.Metadata,
		Timestamp: time.Now().UnixNano(),
	}

	m.sendMessage(msg)
}

// sendMessage encodes and sends a message to the multicast group.
func (m *MulticastDiscovery) sendMessage(msg *DiscoveryMessage) {
	data, err := m.encodeMessage(msg)
	if err != nil {
		return
	}

	addr, err := net.ResolveUDPAddr("udp4", m.config.MulticastGroup+":"+strconv.Itoa(m.config.Port))
	if err != nil {
		return
	}

	// Use a separate connection for sending to avoid interfering with listening
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return
	}
	defer conn.Close()

	_, _ = conn.Write(data) //nolint:gosec // G104: best effort send, error not critical
}

// encodeMessage serializes a message for transmission.
// Wire format with PSK: [magic:4][version:1][sig_len:2][signature:sig_len][json_payload]
// Without PSK: [magic:4][version:1][json_payload].
func (m *MulticastDiscovery) encodeMessage(msg *DiscoveryMessage) ([]byte, error) {
	// Encode payload as JSON
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	// Check if PSK authentication is enabled
	if m.config.ClusterSecret != "" {
		// Compute signature over the payload
		secret := []byte(m.config.ClusterSecret)
		signature := SignMessage(secret, payload)

		// Build message with signature: magic(4) + version(1) + sig_len(2) + signature + payload
		// Signature length is bounded by HMAC-SHA256 output (32 bytes)
		sigLen := uint16(len(signature)) //nolint:gosec // G115: HMAC-SHA256 produces fixed 32-byte output
		data := make([]byte, 4+1+2+len(signature)+len(payload))

		binary.BigEndian.PutUint32(data[0:4], magicNumber)
		data[4] = protocolVersion
		binary.BigEndian.PutUint16(data[5:7], sigLen)
		copy(data[7:7+sigLen], signature)
		copy(data[7+sigLen:], payload)

		return data, nil
	}

	// Build message without signature: magic(4) + version(1) + payload
	data := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(data[0:4], magicNumber)
	data[4] = protocolVersion
	copy(data[5:], payload)

	return data, nil
}

// parseMessage deserializes a received message.
// Verifies the signature if PSK authentication is enabled.
func (m *MulticastDiscovery) parseMessage(data []byte) (*DiscoveryMessage, error) {
	if len(data) < 5 {
		return nil, &MulticastMessageTooShortError{}
	}

	// Check magic number
	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != magicNumber {
		return nil, &MulticastInvalidMagicError{}
	}

	// Check version
	version := data[4]
	if version > protocolVersion {
		return nil, &MulticastUnsupportedVersionError{Version: version}
	}

	var jsonPayload []byte

	// Check if PSK authentication is enabled
	if m.config.ClusterSecret != "" {
		// Message must have signature: magic(4) + version(1) + sig_len(2) + signature + payload
		if len(data) < 7 {
			return nil, &MulticastAuthFormatError{Detail: "message too short for authenticated format"}
		}

		// Extract signature length
		sigLen := binary.BigEndian.Uint16(data[5:7])
		if sigLen == 0 {
			return nil, &MulticastAuthFormatError{Detail: "signature length is zero"}
		}

		// Validate message length
		expectedMinLen := 7 + int(sigLen)
		if len(data) < expectedMinLen {
			return nil, &MulticastAuthFormatError{Detail: "message too short for signature"}
		}

		// Extract signature and payload
		signature := data[7 : 7+sigLen]
		jsonPayload = data[7+sigLen:]

		// Verify signature
		secret := []byte(m.config.ClusterSecret)
		if !VerifySignature(secret, jsonPayload, signature) {
			return nil, &MulticastInvalidSignatureError{}
		}
	} else {
		// No authentication - parse directly
		jsonPayload = data[5:]
	}

	// Parse JSON payload
	var msg DiscoveryMessage
	if err := json.Unmarshal(jsonPayload, &msg); err != nil {
		return nil, err
	}

	msg.Magic = magic
	msg.Version = version

	return &msg, nil
}

// handleMessage processes a received discovery message.
func (m *MulticastDiscovery) handleMessage(msg *DiscoveryMessage) {
	switch msg.Type {
	case MessageAnnounce, MessageResponse:
		m.updatePeer(msg)

	case MessageQuery:
		// Respond to query with our info
		m.sendResponse()

	case MessageLeave:
		m.removePeer(msg.NodeID)
	}
}

// updatePeer adds or updates a peer in the peer map.
func (m *MulticastDiscovery) updatePeer(msg *DiscoveryMessage) {
	if msg.NodeID == 0 || msg.Address == "" {
		return
	}

	peer := Peer{
		NodeID:   msg.NodeID,
		Address:  msg.Address,
		RaftAddr: msg.RaftAddr,
		Metadata: msg.Metadata,
		LastSeen: time.Now(),
		TTL:      m.config.PeerTTL,
	}

	if peer.RaftAddr == "" {
		peer.RaftAddr = peer.Address
	}

	m.peersMu.Lock()
	m.peers[msg.NodeID] = &peerEntry{
		peer:     peer,
		lastSeen: time.Now(),
	}
	m.peersMu.Unlock()
}

// removePeer removes a peer from the map.
func (m *MulticastDiscovery) removePeer(nodeID uint64) {
	m.peersMu.Lock()
	delete(m.peers, nodeID)
	m.peersMu.Unlock()
}

// removeExpiredPeers removes peers that haven't been seen within TTL.
func (m *MulticastDiscovery) removeExpiredPeers() {
	now := time.Now()
	threshold := m.config.PeerTTL

	m.peersMu.Lock()
	for nodeID, entry := range m.peers {
		if now.Sub(entry.lastSeen) > threshold {
			delete(m.peers, nodeID)
		}
	}
	m.peersMu.Unlock()
}

// getPeers returns a slice of current peers.
func (m *MulticastDiscovery) getPeers() []Peer {
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()

	peers := make([]Peer, 0, len(m.peers))
	for _, entry := range m.peers {
		peers = append(peers, entry.peer)
	}

	return peers
}

// GetPeerCount returns the number of discovered peers.
func (m *MulticastDiscovery) GetPeerCount() int {
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()
	return len(m.peers)
}

// IsRunning returns true if the discovery is active.
func (m *MulticastDiscovery) IsRunning() bool {
	return m.running.Load()
}
