// Package conn is one network peer: a reader, a writer, and an acknowledgement
// gate that belongs to this peer alone.
//
// The python this replaces held one gate shared by every connection, so a
// missing acknowledgement from any peer stopped all traffic for five seconds.
// Here a peer waiting on an acknowledgement stalls only itself.
//
// Go enables TCP_NODELAY on every TCPConn, so the Nagle delay the python never
// turned off is already gone.
package conn

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net"
	"sync/atomic"
	"time"
)

// AckTimeout is how long a peer waits before giving up and sending the next
// message anyway.
const AckTimeout = 5 * time.Second

// WatchdogTick is how often a peer is checked for silence, matching the
// python's fifteen seconds. The timeout itself is per connection.
const WatchdogTick = 15 * time.Second

type Conn struct {
	// AckWait is how long this peer holds its gate. Defaults to AckTimeout.
	AckWait time.Duration

	// Watchdog drops the connection after this long with nothing received.
	// Zero leaves it running forever, which is what Home Assistant gets.
	Watchdog time.Duration

	name   string
	net    net.Conn
	split  bufio.SplitFunc
	out    chan outbound
	ack    chan struct{}
	log    *slog.Logger
	lastRx atomic.Int64
}

type outbound struct {
	data    []byte
	wantAck bool
}

func New(name string, c net.Conn, split bufio.SplitFunc, log *slog.Logger) *Conn {
	return &Conn{
		AckWait: AckTimeout,
		name:    name,
		net:     c,
		split:   split,
		out:     make(chan outbound, 64),
		ack:     make(chan struct{}, 1),
		log:     log,
	}
}

func (c *Conn) Name() string { return c.name }

// Close drops the connection, which ends its Run.
func (c *Conn) Close() error { return c.net.Close() }

// Send queues data. It blocks once the queue is full, so a peer that has
// stopped reading slows its sender rather than growing a backlog.
func (c *Conn) Send(data []byte, wantAck bool) {
	c.out <- outbound{data: data, wantAck: wantAck}
}

// Ack releases this peer's gate. Calling it when nothing is waiting is fine.
func (c *Conn) Ack() {
	select {
	case c.ack <- struct{}{}:
	default:
	}
}

// Run reads until the connection ends, handing each whole frame to onFrame,
// and sends queued messages until ctx is cancelled.
func (c *Conn) Run(ctx context.Context, onFrame func([]byte)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Closing the socket is what unblocks a reader parked in Read.
	go func() {
		<-ctx.Done()
		c.net.Close()
	}()

	go c.send(ctx)

	c.lastRx.Store(time.Now().UnixNano())
	if c.Watchdog > 0 {
		go c.watch(ctx)
	}

	s := bufio.NewScanner(c.net)
	s.Split(c.split)

	for s.Scan() {
		c.lastRx.Store(time.Now().UnixNano())
		onFrame(bytes.Clone(s.Bytes()))
	}

	return s.Err()
}

// Idle is how long since anything was received.
func (c *Conn) Idle() time.Duration {
	return time.Since(time.Unix(0, c.lastRx.Load()))
}

// watch drops a peer that has gone quiet. A dead socket often does not report
// itself, so silence is the only signal there is.
func (c *Conn) watch(ctx context.Context) {
	tick := min(WatchdogTick, c.Watchdog/2)

	t := time.NewTicker(tick)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if c.Idle() > c.Watchdog {
				c.log.Info("dropping quiet peer", "conn", c.name, "idle", c.Idle())
				c.Close()
				return
			}
		}
	}
}

func (c *Conn) send(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-c.out:
			if _, err := c.net.Write(m.data); err != nil {
				c.log.Error("write failed", "conn", c.name, "err", err)
				return
			}
			if m.wantAck {
				c.waitAck(ctx)
			}
		}
	}
}

func (c *Conn) waitAck(ctx context.Context) {
	t := time.NewTimer(c.AckWait)
	defer t.Stop()

	select {
	case <-c.ack:
	case <-t.C:
		c.log.Warn("no acknowledgement", "conn", c.name, "waited", c.AckWait)
	case <-ctx.Done():
	}
}
