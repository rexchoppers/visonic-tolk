// Package route decides where a frame goes. It reads no sockets and holds no
// state of its own, so every rule in it is a table test.
package route

import (
	"bytes"

	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
)

type Peer string

const (
	Panel   Peer = "panel"
	Visonic Peer = "visonic"
	Monitor Peer = "monitor"
)

// State is what the decision depends on beyond the frame itself.
type State struct {
	MonitorConnected bool
	VisonicConnected bool
}

type Send struct {
	To      Peer
	WantAck bool
}

// Plan is everything that should happen because of one frame.
type Plan struct {
	Send    []Send
	AckBack bool // acknowledge to whoever sent it
	Drop    bool // close the source connection afterwards
	Local   bool // tolk answers this itself and forwards nothing
}

// Route decides what to do with a frame that arrived from a peer.
func Route(from Peer, f powerlink31.Frame, s State) Plan {
	switch from {
	case Panel:
		return fromPanel(f, s)
	case Visonic:
		return fromVisonic(f)
	case Monitor:
		return fromMonitor(f)
	}
	return Plan{}
}

func fromPanel(f powerlink31.Frame, s State) Plan {
	if isAck(f) {
		if s.VisonicConnected {
			return Plan{Send: []Send{to(Visonic, f)}}
		}
		if s.MonitorConnected && f.Type != powerlink31.TypeAdmAck {
			return Plan{Send: []Send{to(Monitor, f)}}
		}
		return Plan{}
	}

	var p Plan

	// Home Assistant never acknowledges, so nothing sent to it waits.
	if s.MonitorConnected && f.Type != powerlink31.TypeAdmCID {
		p.Send = append(p.Send, Send{To: Monitor})
	}

	if s.VisonicConnected {
		p.Send = append(p.Send, to(Visonic, f))
	} else if f.Type == powerlink31.TypeBBA {
		p.AckBack = true
	}

	return p
}

func fromVisonic(f powerlink31.Frame) Plan {
	if bytes.Equal(f.Data, message.Disconnect) {
		return Plan{AckBack: true, Drop: true}
	}
	return Plan{Send: []Send{to(Panel, f)}}
}

func fromMonitor(f powerlink31.Frame) Plan {
	if message.Class(f.Data) == message.ClassAction {
		return Plan{Local: true, AckBack: true}
	}
	return Plan{Send: []Send{to(Panel, f)}}
}

func to(p Peer, f powerlink31.Frame) Send {
	return Send{To: p, WantAck: waitsForAck(f)}
}

// waitsForAck is the rule from the python's send queue rather than from the
// router: whatever a caller asks for, only these two types ever hold the gate.
func waitsForAck(f powerlink31.Frame) bool {
	return f.Type == powerlink31.TypeBBA || f.Type == powerlink31.TypeAdmCID
}

// IsAck reports whether a frame is an acknowledgement, which is what releases
// the sending peer's gate.
func IsAck(f powerlink31.Frame) bool {
	return isAck(f)
}

func isAck(f powerlink31.Frame) bool {
	switch f.Type {
	case powerlink31.TypeAck, powerlink31.TypeAdmAck, powerlink31.TypeNak:
		return true
	}
	return false
}
