package conn

import (
	"context"
	"log/slog"
	"net"
	"time"
)

// Listen accepts connections on addr until ctx is done, handing each to accept
// in its own goroutine. The panel and Home Assistant both arrive this way.
func Listen(ctx context.Context, addr string, log *slog.Logger, accept func(net.Conn)) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		log.Info("accepted", "addr", addr, "from", c.RemoteAddr())
		go accept(c)
	}
}

// Dial keeps a connection to addr open, calling use with each one and waiting
// retry before trying again after any failure or drop. It returns only when
// ctx is done. The Visonic cloud is reached this way.
func Dial(ctx context.Context, addr string, retry time.Duration, log *slog.Logger, use func(net.Conn)) {
	for {
		c, err := new(net.Dialer).DialContext(ctx, "tcp", addr)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("dial failed", "addr", addr, "err", err, "retry", retry)
		} else {
			log.Info("connected", "addr", addr)
			use(c)
			c.Close()

			if ctx.Err() != nil {
				return
			}
			log.Info("disconnected", "addr", addr, "retry", retry)
		}

		if !wait(ctx, retry) {
			return
		}
	}
}

func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
