package manager

import (
	"fmt"
	"math/rand"

	"github.com/pion/webrtc/v3"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

const (
	idLen = 32
)

var (
	letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

	sessionGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "infisk8_sessions",
		Help: "Current number of sessions",
	})

	poolGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "infisk8_pools",
		Help: "Current number of pools",
	})

	messageSentCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "infisk8_messages_sent_total",
		Help: "Total number of messages sent",
	})

	messageReceivedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "infisk8_messages_received_total",
		Help: "Total number of messages received",
	})
)

func init() {
	prometheus.MustRegister(sessionGauge)
	prometheus.MustRegister(poolGauge)
	prometheus.MustRegister(messageSentCounter)
	prometheus.MustRegister(messageReceivedCounter)
}

func genID() string {
	c := make([]rune, idLen)
	for i := range c {
		c[i] = letters[rand.Intn(len(letters))]
	}
	return string(c)
}

// Manager manages pools
type Manager struct {
	logger log.Logger
	pools  *map[string]*Pool
}

func NewManager(logger log.Logger) *Manager {
	m := &Manager{
		logger: logger,
		pools:  &map[string]*Pool{},
	}
	// FIXME: Remove
	m.NewPool("test")
	return m
}

func (m *Manager) Pools() []string {
	ps := make([]string, len(*m.pools))
	i := 0
	for n, _ := range *m.pools {
		ps[i] = n
		i++
	}
	return ps
}

// Retrieves pool by name, returns error if not found.
func (m *Manager) Pool(name string) (*Pool, error) {
	p, ok := (*m.pools)[name]
	if !ok {
		return nil, fmt.Errorf("Couldn't find pool with name %s", name)
	}
	return p, nil
}

func (m *Manager) NewPool(name string) (*Pool, error) {
	_, ok := (*m.pools)[name]
	if ok {
		return nil, fmt.Errorf("Pool with name %s already exists", name)
	}
	p := &Pool{
		logger: log.With(m.logger, "pool", name),
		config: webrtc.Configuration{
			ICEServers: []webrtc.ICEServer{
				{
					URLs: []string{"stun:stun.l.google.com:19302"},
				},
			},
		},
		sessions: &map[string]*Session{},
	}
	(*m.pools)[name] = p
	poolGauge.Set(float64(len(*m.pools)))
	return p, nil
}

// Pool manages sessions
type Pool struct {
	logger   log.Logger
	config   webrtc.Configuration
	sessions *map[string]*Session
}

func (r *Pool) NewSession(sd []byte, id string) (webrtc.SessionDescription, error) {
	session, err := NewSession(r, id)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	(*r.sessions)[id] = session
	sessionGauge.Set(float64(len(*r.sessions)))
	return session.Connect(sd)
}

func (p *Pool) CloseSession(id string) error {
	session, ok := (*p.sessions)[id]
	if !ok {
		return fmt.Errorf("Couldn't find session with id %s", id)
	}
	if err := session.pc.Close(); err != nil {
		return err
	}
	delete(*p.sessions, id)
	sessionGauge.Set(float64(len(*p.sessions)))
	return nil
}

func (p *Pool) Broadcast(cid, label string, data []byte) {
	for id, s := range *p.sessions {
		if !s.open {
			continue
		}
		if id == cid { // No need to broadcast to ourselves
			continue
		}
		if rand.Intn(100) < 1 {
			level.Debug(p.logger).Log("msg", "<", "id", id, "data", string(data))
		}
		messageSentCounter.Inc()
		if err := s.dc[label].Send(data); err != nil {
			level.Warn(p.logger).Log("msg", "Couldn't send data", "error", err, "id", id)
		}
		// FIXME: Consider binary
		/*
			if err := s.dc.Send(datachannel.PayloadBinary{Data: data}); err != nil {
				level.Warn(p.logger).Log("msg", "Couldn't send data", "error", err, "id", id)
			}*/
	}
}

// Session is a session with a client, can have multiple datachannels
type Session struct {
	logger log.Logger
	*Pool
	ID   string
	open bool
	pc   *webrtc.PeerConnection
	dc   map[string]*webrtc.DataChannel
}

func NewSession(pool *Pool, id string) (*Session, error) {
	pc, err := webrtc.NewPeerConnection(pool.config)
	if err != nil {
		return nil, err
	}

	p := &Session{
		logger: log.With(pool.logger, "session", id),
		Pool:   pool,
		ID:     id,
		pc:     pc,
		dc:     make(map[string]*webrtc.DataChannel),
	}

	pc.OnConnectionStateChange(p.OnConnectionStateChange)
	pc.OnDataChannel(p.OnDataChannel)
	return p, nil
}

func (p *Session) OnConnectionStateChange(connectionState webrtc.PeerConnectionState) {
	level.Info(p.logger).Log("msg", "ICE Connection State has changed", "connectionState", connectionState.String())
	switch connectionState {
	case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateClosed:
		if err := p.Pool.CloseSession(p.ID); err != nil {
			level.Error(p.logger).Log("msg", "Couldn't close session", "error", err)
		}
	}
}

func (p *Session) OnDataChannel(d *webrtc.DataChannel) {
	p.dc[d.Label()] = d
	level.Info(p.logger).Log("msg", "New data channel", "label", d.Label, "id", d.ID)

	d.OnOpen(p.OnOpen)

	d.OnMessage(func(message webrtc.DataChannelMessage) { p.OnMessage(d.Label(), message) })
	// d.Onmessage = d.OnMessage // FIXME: Upstream bug?
}

func (p *Session) OnMessage(label string, message webrtc.DataChannelMessage) {
	messageReceivedCounter.Inc()
	p.Pool.Broadcast(p.ID, label, message.Data)
}

// OnOpen is called when a connection was established and updates clients
func (p *Session) OnOpen() {
	p.open = true
}

func (p *Session) Connect(sd []byte) (webrtc.SessionDescription, error) {
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP: string(sd),
	}
	/*
	if err := json.Unmarshal(sd, &offer); err != nil {
		return webrtc.SessionDescription{}, err
	}*/
	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return webrtc.SessionDescription{}, fmt.Errorf("Couldn't set remove description: %w", err)
	}
	return p.pc.CreateAnswer(nil)
}
