package tolk

import (
	"bytes"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/message"
	"github.com/rexchoppers/visonic-tolk/internal/powerlink31"
)

// sendStatus tells Home Assistant what tolk can currently see. The python
// sends one whenever a connection comes or goes, so the integration never has
// to ask to find out something changed.
func (t *Tolk) sendStatus() {
	t.mu.RLock()
	panels, monitors := len(t.panels), len(t.monitors)
	clouds := 0
	for _, p := range t.panels {
		if p.visonic != nil {
			clouds++
		}
	}
	stealth, downloading := t.stealth, t.download
	t.mu.RUnlock()

	if monitors == 0 {
		return
	}

	t.toMonitors(message.Status(panels, clouds, monitors, true, stealth, downloading))
}

// action answers an e1 command from Home Assistant.
//
//	in: 0d e1 <command> <value> 43 <checksum> 0a
func (t *Tolk) action(f powerlink31.Frame) {
	if len(f.Data) < 4 {
		t.log.Warn("action too short to read", "data", f.Data)
		return
	}

	command, value := f.Data[2], f.Data[3]
	t.log.Debug("action", "command", command, "value", value)

	switch command {
	case 0x01:
		t.sendStatus()
	case 0x02:
		t.setStealth(value == 0x01)
	default:
		t.log.Warn("unknown action", "command", command, "value", value)
	}
}

// noteDownload watches for Home Assistant starting or ending an eprom read,
// which takes the cloud out of the way for the duration.
func (t *Tolk) noteDownload(data []byte) {
	switch {
	case bytes.Equal(data, message.Download) || message.Class(data) == message.ClassDownload:
		t.mu.Lock()
		t.download = true
		t.mu.Unlock()
		t.setStealth(true)

	case bytes.Equal(data, message.ExitDownload):
		t.mu.Lock()
		t.download = false
		t.mu.Unlock()
		t.setStealth(false)
	}
}

func (t *Tolk) inStealth() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.stealth
}

// setStealth keeps the cloud away, or lets it back. Entering ends every cloud
// dial; leaving starts fresh ones.
func (t *Tolk) setStealth(on bool) {
	t.mu.Lock()
	changed := t.stealth != on
	t.stealth = on
	base := t.base
	ps := make([]*panel, 0, len(t.panels))
	for _, p := range t.panels {
		ps = append(ps, p)
	}
	t.mu.Unlock()

	if on {
		t.holdStealth()
	}

	if !changed {
		return
	}

	if on {
		t.log.Info("entering stealth, the cloud is kept away")
		for _, p := range ps {
			t.mu.Lock()
			stop := p.stopCloud
			p.stopCloud = nil
			t.mu.Unlock()

			if stop != nil {
				stop()
			}
		}
	} else {
		t.log.Info("leaving stealth")
		t.releaseStealth()
		for _, p := range ps {
			t.dialCloud(base, p)
		}
	}

	t.sendStatus()
}

// holdStealth pushes the safety timeout out again. Home Assistant has to keep
// asking, so a client that goes away does not strand the panel without its
// cloud link.
func (t *Tolk) holdStealth() {
	if t.cfg.StealthTimeout <= 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stealthUntil != nil {
		t.stealthUntil.Reset(t.cfg.StealthTimeout)
		return
	}

	t.stealthUntil = time.AfterFunc(t.cfg.StealthTimeout, func() {
		t.log.Info("stealth timed out, letting the cloud back")
		t.setStealth(false)
	})
}

func (t *Tolk) releaseStealth() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stealthUntil != nil {
		t.stealthUntil.Stop()
		t.stealthUntil = nil
	}
}
