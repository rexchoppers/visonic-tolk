// Package tolk assembles the peers: it accepts panels and Home Assistant,
// dials a cloud link for each panel, and acts on what the router decides.
package tolk

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/conn"
	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
	"github.com/rexchoppers/visonic-tolk/internal/route"
	"github.com/rexchoppers/visonic-tolk/internal/web"
)

type Config struct {
	PanelAddr   string
	MonitorAddr string
	VisonicAddr string
	Reconnect   time.Duration

	// Keepalive is how quiet the panel may go before tolk pokes it. Only used
	// while the cloud is down, because otherwise the cloud's own traffic keeps
	// the panel busy.
	Keepalive time.Duration

	// Watchdog drops a panel or cloud link that has said nothing for this long.
	Watchdog time.Duration

	// StealthTimeout lets the cloud back if Home Assistant stops asking for
	// stealth, so a client that disappears mid download cannot strand a panel.
	StealthTimeout time.Duration

	// WebAddr is where the panel checks in over https. Its reply is what tells
	// the panel to open its message connection, so with this unset the panel
	// never connects at all.
	WebAddr string

	// WebUpstream is the https address Visonic answers those check-ins on.
	WebUpstream string

	// CertDir holds the self signed certificate the panel is served.
	CertDir string
}

// panel is one alarm panel and the cloud link that belongs to it.
type panel struct {
	id      int
	conn    *conn.Conn
	visonic *conn.Conn
	account string
	panelID string

	// stopCloud ends the dial loop, which is how stealth mode keeps the cloud
	// away rather than dialling and hanging up over and over.
	stopCloud context.CancelFunc
}

type Tolk struct {
	cfg Config
	log *slog.Logger

	mu       sync.RWMutex
	panels   map[int]*panel
	monitors []*conn.Conn
	nextID   int
	nextMsg  int
	stealth  bool
	download bool

	stealthUntil *time.Timer

	// base is kept so leaving stealth can start a fresh cloud dial after the
	// old one was cancelled.
	base context.Context
}

func New(cfg Config, log *slog.Logger) *Tolk {
	return &Tolk{cfg: cfg, log: log, panels: map[int]*panel{}}
}

func (t *Tolk) Run(ctx context.Context) error {
	t.mu.Lock()
	t.base = ctx
	t.mu.Unlock()

	errs := make(chan error, 3)

	go func() {
		errs <- conn.Listen(ctx, t.cfg.PanelAddr, t.log, func(c net.Conn) { t.servePanel(ctx, c) })
	}()
	go func() {
		errs <- conn.Listen(ctx, t.cfg.MonitorAddr, t.log, func(c net.Conn) { t.serveMonitor(ctx, c) })
	}()

	if t.cfg.WebAddr != "" {
		go func() {
			errs <- web.New(web.Config{
				Addr:        t.cfg.WebAddr,
				Upstream:    t.cfg.WebUpstream,
				ConnectPort: portOf(t.cfg.PanelAddr),
				CertDir:     t.cfg.CertDir,
			}, t.log).Serve(ctx)
		}()
	}

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		return nil
	}
}

func (t *Tolk) servePanel(ctx context.Context, c net.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pc := conn.New("panel", c, powerlink31.SplitFrames, t.log)
	pc.Watchdog = t.cfg.Watchdog

	t.mu.Lock()
	t.nextID++
	p := &panel{id: t.nextID, conn: pc}
	t.panels[p.id] = p
	t.mu.Unlock()

	go t.keepalive(ctx, p)
	t.dialCloud(ctx, p)
	t.sendStatus()

	p.conn.Run(ctx, func(b []byte) { t.onFrame(route.Panel, p.conn, p, b) })

	t.mu.Lock()
	delete(t.panels, p.id)
	t.mu.Unlock()

	t.log.Info("panel gone", "id", p.id)
	t.sendStatus()
}

// dialCloud keeps this panel's cloud link up under a context of its own, so
// stealth mode can end it without touching the panel connection.
func (t *Tolk) dialCloud(parent context.Context, p *panel) {
	if t.inStealth() {
		return
	}

	ctx, cancel := context.WithCancel(parent)

	t.mu.Lock()
	p.stopCloud = cancel
	t.mu.Unlock()

	go conn.Dial(ctx, t.cfg.VisonicAddr, t.cfg.Reconnect, t.log, func(vc net.Conn) {
		v := conn.New("visonic", vc, powerlink31.SplitFrames, t.log)
		v.Watchdog = t.cfg.Watchdog

		t.mu.Lock()
		p.visonic = v
		t.mu.Unlock()
		t.sendStatus()

		v.Run(ctx, func(b []byte) { t.onFrame(route.Visonic, v, p, b) })

		t.mu.Lock()
		p.visonic = nil
		t.mu.Unlock()
		t.sendStatus()
	})
}

func (t *Tolk) serveMonitor(ctx context.Context, c net.Conn) {
	m := conn.New("monitor", c, message.Split, t.log)

	t.mu.Lock()
	t.monitors = append(t.monitors, m)
	t.mu.Unlock()
	t.sendStatus()

	m.Run(ctx, func(b []byte) { t.onMonitor(m, b) })

	t.mu.Lock()
	t.monitors = remove(t.monitors, m)
	t.mu.Unlock()

	t.log.Info("monitor gone")
	t.sendStatus()
}

// keepalive pokes a quiet panel, but only while its cloud link is down. With
// the cloud connected the panel is already being talked to, and the python
// only runs this in the same circumstance.
func (t *Tolk) keepalive(ctx context.Context, p *panel) {
	if t.cfg.Keepalive <= 0 {
		return
	}

	tick := time.NewTicker(t.cfg.Keepalive / 4)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if t.visonicOf(p) != nil || p.conn.Idle() < t.cfg.Keepalive {
				continue
			}

			t.log.Debug("poking a quiet panel", "id", p.id, "idle", p.conn.Idle())
			p.conn.Send(powerlink31.Frame{
				Type:    powerlink31.TypeBBA,
				MsgID:   t.msgID(),
				Account: p.account,
				Panel:   p.panelID,
				Data:    message.Keepalive,
			}.Encode(), false)
		}
	}
}

// onFrame handles a whole powerlink31 frame from a panel or its cloud link.
func (t *Tolk) onFrame(from route.Peer, src *conn.Conn, p *panel, raw []byte) {
	f, err := powerlink31.Decode(raw)
	if err != nil {
		t.log.Warn("undecodable frame", "from", from, "err", err, "bytes", raw)
		return
	}

	// This peer has answered, so let its next message go. Any frame counts,
	// not only an acknowledgement: the python releases on an ack, and releases
	// again on a normal message from the same peer, because a panel that
	// answers with data rather than an ack would otherwise hold the gate shut
	// for the full timeout. Released before routing, so forwarding never holds
	// up the reply.
	src.Ack()

	t.log.Debug("rx",
		"from", from,
		"type", f.Type,
		"id", f.MsgID,
		"class", fmt.Sprintf("%02x", message.Class(f.Data)),
		"data", asHex(f.Data),
	)

	if from == route.Panel {
		t.remember(p, f)
	}

	t.apply(from, p, f, route.Route(from, f, t.state(p)))
}

// onMonitor handles bare panel messages from Home Assistant, which are wrapped
// into a frame before anything else looks at them.
func (t *Tolk) onMonitor(src *conn.Conn, raw []byte) {
	data, isAck, err := message.FromMonitor(raw)
	if err != nil {
		t.log.Warn("unsupported from home assistant", "err", err, "data", asHex(raw))
		return
	}

	t.log.Debug("rx",
		"from", route.Monitor,
		"class", fmt.Sprintf("%02x", message.Class(data)),
		"ack", isAck,
		"data", asHex(data),
	)

	if isAck {
		src.Ack()
	}

	t.noteDownload(data)

	// An action is addressed to tolk, so it is answered whether or not a panel
	// is connected. Everything else needs somewhere to go.
	if message.Class(data) == message.ClassAction {
		t.action(powerlink31.Frame{Data: data})
		return
	}

	p := t.firstPanel()
	if p == nil {
		t.log.Warn("no panel connected, dropping", "bytes", raw)
		return
	}

	typ := powerlink31.TypeBBA
	if isAck {
		typ = powerlink31.TypeAck
	}

	f := powerlink31.Frame{
		Type:    typ,
		MsgID:   t.msgID(),
		Account: p.account,
		Panel:   p.panelID,
		Data:    data,
	}
	f.Raw = f.Encode()

	t.apply(route.Monitor, p, f, route.Route(route.Monitor, f, t.state(p)))
}

func (t *Tolk) apply(from route.Peer, p *panel, f powerlink31.Frame, plan route.Plan) {
	for _, s := range plan.Send {
		t.log.Debug("tx",
			"to", s.To,
			"type", f.Type,
			"id", f.MsgID,
			"waits", s.WantAck,
			"data", asHex(f.Data),
		)

		switch s.To {
		case route.Monitor:
			t.toMonitors(f.Data)
		case route.Visonic:
			if v := t.visonicOf(p); v != nil {
				v.Send(f.Raw, s.WantAck)
			}
		case route.Panel:
			p.conn.Send(f.Raw, s.WantAck)
		}
	}

	if plan.AckBack {
		t.ackBack(from, p, f)
	}

	if plan.Local {
		t.action(f)
	}

	if plan.Drop {
		if v := t.visonicOf(p); v != nil {
			v.Close()
		}
	}
}

func (t *Tolk) ackBack(to route.Peer, p *panel, f powerlink31.Frame) {
	ack := powerlink31.Frame{
		Type:    powerlink31.TypeAck,
		MsgID:   f.MsgID,
		Account: p.account,
		Panel:   p.panelID,
		Data:    message.PLAck,
	}.Encode()

	switch to {
	case route.Panel:
		p.conn.Send(ack, false)
	case route.Visonic:
		if v := t.visonicOf(p); v != nil {
			v.Send(ack, false)
		}
	case route.Monitor:
		t.toMonitors(message.PLAck)
	}
}

// remember keeps the addressing the panel uses, so a message built here can
// carry the same account and panel back to it.
func (t *Tolk) remember(p *panel, f powerlink31.Frame) {
	if f.Account == "" || f.Account == "0" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	p.account, p.panelID = f.Account, f.Panel
}

func (t *Tolk) toMonitors(data []byte) {
	t.mu.RLock()
	ms := make([]*conn.Conn, len(t.monitors))
	copy(ms, t.monitors)
	t.mu.RUnlock()

	for _, m := range ms {
		m.Send(data, false)
	}
}

func (t *Tolk) state(p *panel) route.State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return route.State{
		MonitorConnected: len(t.monitors) > 0,
		VisonicConnected: p.visonic != nil,
	}
}

func (t *Tolk) visonicOf(p *panel) *conn.Conn {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return p.visonic
}

// firstPanel is where Home Assistant's messages go. The python forwards them
// to client id 0, which it documents as the first connection of that name, so
// with more than one panel only the first is reachable from Home Assistant.
func (t *Tolk) firstPanel() *panel {
	t.mu.RLock()
	defer t.mu.RUnlock()

	best := 0
	var found *panel
	for id, p := range t.panels {
		if best == 0 || id < best {
			best, found = id, p
		}
	}
	return found
}

func (t *Tolk) msgID() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nextMsg++
	if t.nextMsg > 9999 {
		t.nextMsg = 1
	}
	return t.nextMsg
}

// portOf takes the port out of a listen address, so the panel is told to
// connect to the port tolk is actually listening on.
func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return strings.TrimPrefix(addr, ":")
}

// asHex renders bytes the way the python logs them, spaced, so a line here can
// be put beside a line from the old app.
func asHex(b []byte) string {
	var s strings.Builder
	for i, x := range b {
		if i > 0 {
			s.WriteByte(' ')
		}
		fmt.Fprintf(&s, "%02x", x)
	}
	return s.String()
}

func remove(cs []*conn.Conn, c *conn.Conn) []*conn.Conn {
	for i, x := range cs {
		if x == c {
			return append(cs[:i], cs[i+1:]...)
		}
	}
	return cs
}
