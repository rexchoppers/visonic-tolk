// Package tolk assembles the peers: it accepts panels and Home Assistant,
// dials a cloud link for each panel, and acts on what the router decides.
package tolk

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/conn"
	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
	"github.com/rexchoppers/visonic-tolk/internal/route"
)

type Config struct {
	PanelAddr   string
	MonitorAddr string
	VisonicAddr string
	Reconnect   time.Duration
}

// panel is one alarm panel and the cloud link that belongs to it.
type panel struct {
	id      int
	conn    *conn.Conn
	visonic *conn.Conn
	account string
	panelID string
}

type Tolk struct {
	cfg Config
	log *slog.Logger

	mu       sync.RWMutex
	panels   map[int]*panel
	monitors []*conn.Conn
	nextID   int
	nextMsg  int
}

func New(cfg Config, log *slog.Logger) *Tolk {
	return &Tolk{cfg: cfg, log: log, panels: map[int]*panel{}}
}

func (t *Tolk) Run(ctx context.Context) error {
	errs := make(chan error, 2)

	go func() {
		errs <- conn.Listen(ctx, t.cfg.PanelAddr, t.log, func(c net.Conn) { t.servePanel(ctx, c) })
	}()
	go func() {
		errs <- conn.Listen(ctx, t.cfg.MonitorAddr, t.log, func(c net.Conn) { t.serveMonitor(ctx, c) })
	}()

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

	t.mu.Lock()
	t.nextID++
	p := &panel{id: t.nextID, conn: conn.New("panel", c, powerlink31.SplitFrames, t.log)}
	t.panels[p.id] = p
	t.mu.Unlock()

	go conn.Dial(ctx, t.cfg.VisonicAddr, t.cfg.Reconnect, t.log, func(vc net.Conn) {
		v := conn.New("visonic", vc, powerlink31.SplitFrames, t.log)

		t.mu.Lock()
		p.visonic = v
		t.mu.Unlock()

		v.Run(ctx, func(b []byte) { t.onFrame(route.Visonic, p, b) })

		t.mu.Lock()
		p.visonic = nil
		t.mu.Unlock()
	})

	p.conn.Run(ctx, func(b []byte) { t.onFrame(route.Panel, p, b) })

	t.mu.Lock()
	delete(t.panels, p.id)
	t.mu.Unlock()
	t.log.Info("panel gone", "id", p.id)
}

func (t *Tolk) serveMonitor(ctx context.Context, c net.Conn) {
	m := conn.New("monitor", c, conn.SplitRead, t.log)

	t.mu.Lock()
	t.monitors = append(t.monitors, m)
	t.mu.Unlock()

	m.Run(ctx, func(b []byte) { t.onMonitor(b) })

	t.mu.Lock()
	t.monitors = remove(t.monitors, m)
	t.mu.Unlock()
	t.log.Info("monitor gone")
}

// onFrame handles a whole powerlink31 frame from a panel or its cloud link.
func (t *Tolk) onFrame(from route.Peer, p *panel, raw []byte) {
	f, err := powerlink31.Decode(raw)
	if err != nil {
		t.log.Warn("undecodable frame", "from", from, "err", err, "bytes", raw)
		return
	}

	if from == route.Panel {
		t.remember(p, f)
	}

	t.apply(from, p, f, route.Route(from, f, t.state(p)))
}

// onMonitor handles bare panel messages from Home Assistant, which are wrapped
// into a frame before anything else looks at them.
func (t *Tolk) onMonitor(raw []byte) {
	data, isAck, err := message.FromMonitor(raw)
	if err != nil {
		t.log.Warn("unusable message from home assistant", "err", err, "bytes", raw)
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
		t.log.Info("action from home assistant is not handled yet", "data", f.Data)
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

func remove(cs []*conn.Conn, c *conn.Conn) []*conn.Conn {
	for i, x := range cs {
		if x == c {
			return append(cs[:i], cs[i+1:]...)
		}
	}
	return cs
}
