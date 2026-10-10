package tolk

import (
	"slices"

	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
	"github.com/rexchoppers/visonic-tolk/internal/route"
)

// greet asks the panel for its state when a cloud link comes back, so Visonic
// starts with what it missed rather than silence. The python leaves the first
// link after starting alone, and any link that is not the only one, since a
// second panel connection carries alarm reports rather than a session.
func (t *Tolk) greet(p *panel) {
	t.mu.Lock()
	first := !t.cloudSeen
	t.cloudSeen = true

	clouds := 0
	for _, q := range t.panels {
		if q.visonic != nil {
			clouds++
		}
	}

	if first || len(t.panels) != 1 || clouds != 1 || p.account == "" {
		t.mu.Unlock()
		return
	}

	account, panelID, asks := p.account, p.panelID, p.asks
	p.asks = nil
	t.mu.Unlock()

	id := t.msgID()
	data := message.Init(asks)

	t.mu.Lock()
	p.initID = id
	t.mu.Unlock()

	t.log.Info("greeting the returning cloud", "id", id)
	t.log.Debug("tx", "to", route.Panel, "type", powerlink31.TypeBBA, "id", id, "waits", true, "data", asHex(data))

	p.conn.Send(powerlink31.Frame{
		Type:    powerlink31.TypeBBA,
		MsgID:   id,
		Account: account,
		Panel:   panelID,
		Data:    data,
	}.Encode(), true)
}

// noteAsks keeps what the panel announces while its cloud link is down, so the
// greeting can ask for it once the link is back.
func (t *Tolk) noteAsks(p *panel, data []byte) {
	asks := message.Asks(data)
	if asks == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if p.visonic != nil {
		return
	}
	for _, a := range asks {
		if a != 0x51 && !slices.Contains(p.asks, a) {
			p.asks = append(p.asks, a)
		}
	}
}

// ownAck reports whether f answers tolk's greeting. Visonic never sent that
// message, so it must not see the answer.
func (t *Tolk) ownAck(p *panel, f powerlink31.Frame) bool {
	if !route.IsAck(f) {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if p.initID == 0 || f.MsgID != p.initID {
		return false
	}
	p.initID = 0
	return true
}
