package manager

import (
	"fmt"
	"math/rand"

	"github.com/pions/webrtc"
	"github.com/pions/webrtc/pkg/datachannel"
	"github.com/pions/webrtc/pkg/ice"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

const (
	idLen = 32
)

var (
	letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
)

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
	return &Manager{
		logger: logger,
		pools:  &map[string]*Pool{},
	}
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
		config: webrtc.RTCConfiguration{
			IceServers: []webrtc.RTCIceServer{
				{
					URLs: []string{"stun:stun.l.google.com:19302"},
				},
			},
		},
		sessions: &map[string]*Session{},
	}
	(*m.pools)[name] = p
	return p, nil
}

// Pool manages sessions
type Pool struct {
	logger   log.Logger
	config   webrtc.RTCConfiguration
	sessions *map[string]*Session
}

func (r *Pool) NewSession(sd string) (webrtc.RTCSessionDescription, error) {
	session, err := NewSession(r)
	if err != nil {
		return webrtc.RTCSessionDescription{}, err
	}
	(*r.sessions)[session.ID] = session
	return session.Connect(sd)
}

func (p *Pool) Broadcast(cid string, data []byte) {
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
		if err := s.dc.Send(datachannel.PayloadString{Data: data}); err != nil {
			level.Warn(p.logger).Log("msg", "Couldn't send data", "error", err, "id", id)
		}
		// FIXME: Consider binary
		/*
			if err := s.dc.Send(datachannel.PayloadBinary{Data: data}); err != nil {
				level.Warn(p.logger).Log("msg", "Couldn't send data", "error", err, "id", id)
			}*/
	}
}

// Session is a session with a client
type Session struct {
	logger log.Logger
	*Pool

	open bool
	ID   string
	pc   *webrtc.RTCPeerConnection
	dc   *webrtc.RTCDataChannel
}

func NewSession(pool *Pool) (*Session, error) {
	pc, err := webrtc.New(pool.config)
	if err != nil {
		return nil, err
	}

	p := &Session{
		logger: pool.logger,
		ID:     genID(),
		Pool:   pool,
		pc:     pc,
	}

	pc.OnICEConnectionStateChange = p.OnICEConnectionStateChange
	pc.OnDataChannel = p.OnDataChannel
	return p, nil
}

func (p *Session) OnICEConnectionStateChange(connectionState ice.ConnectionState) {
	level.Info(p.logger).Log("msg", "ICE Connection State has changed", "connectionState", connectionState.String())
}

func (p *Session) OnDataChannel(d *webrtc.RTCDataChannel) {
	p.dc = d
	level.Info(p.logger).Log("msg", "New data channel", "label", d.Label, "id", d.ID)

	d.OnOpen = p.OnOpen

	d.OnMessage = p.OnMessage
	d.Onmessage = d.OnMessage // FIXME: Upstream bug?
}

func (p *Session) OnMessage(payload datachannel.Payload) {
	switch pt := payload.(type) {
	case *datachannel.PayloadString:
		p.Pool.Broadcast(p.ID, pt.Data)
	case *datachannel.PayloadBinary:
		p.Pool.Broadcast(p.ID, pt.Data)
	default:
		fmt.Printf("Message '%s' from DataChannel '%s' no payload \n", pt.PayloadType().String(), p.dc.Label)
	}
}

// OnOpen is called when a connection was established and updates clients
func (p *Session) OnOpen() {
	p.open = true
}

func (p *Session) Connect(sd string) (webrtc.RTCSessionDescription, error) {
	offer := webrtc.RTCSessionDescription{
		Type: webrtc.RTCSdpTypeOffer,
		Sdp:  string(sd),
	}
	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return webrtc.RTCSessionDescription{}, err
	}
	return p.pc.CreateAnswer(nil)
}
