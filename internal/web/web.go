// Package web is the https side the panel checks in on.
//
// It matters far more than it looks. The panel posts to /scripts/update.php,
// and the reply is where it is told to open its message connection. tolk
// forwards the post to Visonic and adds a connect command to the answer, so
// the panel dials tolk rather than only ever talking to the cloud. Without
// this the panel never connects at all.
package web

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"
)

// updatePath is the check-in the connect command is added to.
const updatePath = "/scripts/update.php"

// filmPath carries camera stills, which are passed through untouched.
const filmPath = "/scripts/pir_film.php"

type Config struct {
	Addr        string // where the panel reaches tolk, :8443
	Upstream    string // https://host:port that Visonic answers on
	ConnectPort string // the port the panel is told to open, 5001
	CertDir     string // where the self signed certificate is kept

	// KaTime is the seconds the panel is told to wait between check-ins. Ten
	// is what the python sends. Whether it paces anything else the panel does
	// is unknown, so it is exposed to be experimented with.
	KaTime int
}

type Server struct {
	cfg Config
	log *slog.Logger
	up  *http.Client
}

func New(cfg Config, log *slog.Logger) *Server {
	if cfg.KaTime <= 0 {
		cfg.KaTime = 10
	}

	return &Server{
		cfg: cfg,
		log: log,
		// No overall deadline on the client. The python's two seconds is a
		// connect and between-reads timeout, while Go's would cap the whole
		// exchange, which cuts off camera uploads part way. The limits that
		// matter are set per stage below, and per request in forward.
		up: &http.Client{
			Transport: &http.Transport{
				// Visonic presents a certificate nothing here can verify, and
				// the python does the same.
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
				DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
				TLSHandshakeTimeout:   2 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
			},
		},
	}
}

func (s *Server) Serve(ctx context.Context) error {
	cert, err := ensureCert(s.cfg.CertDir)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:      s.cfg.Addr,
		Handler:   http.HandlerFunc(s.handle),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
		ErrorLog:  nil,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	l, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}

	s.log.Info("web listening", "addr", s.cfg.Addr)

	if err := srv.ServeTLS(l, "", ""); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	s.log.Debug("web rx", "path", r.URL.Path, "bytes", len(body))

	res, err := s.forward(r, body)
	if err != nil {
		// Only the check-in is answered alone, because the panel has to be told
		// to connect or it sits there forever. Anything else, a camera upload
		// above all, is left to the panel to retry rather than being handed a
		// reply meant for a different request.
		if r.URL.Path != updatePath {
			s.log.Warn("visonic unreachable", "path", r.URL.Path, "err", err)
			http.Error(w, "", http.StatusBadGateway)
			return
		}

		s.log.Warn("visonic unreachable, telling the panel to connect anyway", "err", err)
		s.write(w, http.StatusOK, nil, s.connectCommand(nil))
		return
	}
	out := res.body
	if r.URL.Path == updatePath {
		out = s.connectCommand(res.body)
	}

	if r.URL.Path == filmPath {
		s.log.Info("camera still forwarded", "sent", len(body), "answer", res.status)
	}

	s.write(w, res.status, res.headers, out)
}

// reply is the upstream answer, read out in full so the request context can be
// released before the caller uses it.
type reply struct {
	status  int
	headers http.Header
	body    []byte
}

func (s *Server) forward(r *http.Request, body []byte) (*reply, error) {
	// The panel waits on the check-in, so it gets a short deadline. A camera
	// upload is large and gets room to finish.
	wait := 30 * time.Second
	if r.URL.Path == updatePath {
		wait = 2 * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), wait)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, r.Method, s.cfg.Upstream+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.URL.RawQuery = r.URL.RawQuery
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	res, err := s.up.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	out, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return &reply{status: res.StatusCode, headers: res.Header, body: out}, nil
}

// connectCommand rewrites Visonic's answer so it carries the instruction that
// makes the panel open its message connection to tolk.
func (s *Server) connectCommand(upstream []byte) []byte {
	out := map[string]any{}
	if len(upstream) > 0 {
		if err := json.Unmarshal(upstream, &out); err != nil {
			s.log.Debug("visonic answered with something that is not json", "bytes", len(upstream))
			out = map[string]any{}
		}
	}

	out["cmds"] = []map[string]any{{
		"name":   "connect",
		"params": map[string]any{"port": port(s.cfg.ConnectPort)},
	}}
	out["ka_time"] = s.cfg.KaTime
	out["version"] = 3

	b, err := json.Marshal(out)
	if err != nil {
		return upstream
	}

	s.log.Info("telling the panel to connect", "port", s.cfg.ConnectPort)
	return append(b, '\n')
}

func (s *Server) write(w http.ResponseWriter, status int, headers http.Header, body []byte) {
	for k, vs := range headers {
		if k == "Content-Length" {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

func port(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 5001
	}
	return n
}
