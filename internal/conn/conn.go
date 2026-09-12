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
	"time"
)

// AckTimeout is how long a peer waits before giving up and sending the next
// message anyway.
const AckTimeout = 5 * time.Second

type Conn struct {
	// AckWait is how long this peer holds its gate. Defaults to AckTimeout.
	AckWait time.Duration

	name  string
	net   net.Conn
	split bufio.SplitFunc
	out   chan outbound
	ack   chan struct{}
	log   *slog.Logger
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

	s := bufio.NewScanner(c.net)
	s.Split(c.split)

	for s.Scan() {
		onFrame(bytes.Clone(s.Bytes()))
	}

	return s.Err()
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
