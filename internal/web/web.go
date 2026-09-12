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
}

type Server struct {
	cfg Config
	log *slog.Logger
	up  *http.Client
}

func New(cfg Config, log *slog.Logger) *Server {
	return &Server{
		cfg: cfg,
		log: log,
		up: &http.Client{
			// Two seconds, as the python uses. The panel is waiting on this
			// reply, so a slow cloud must not hold up the connect command.
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				// Visonic presents a certificate nothing here can verify, and
				// the python does the same.
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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
		// Visonic is unreachable, so answer on its behalf. The panel still has
		// to be told to connect, or it will sit there forever.
		s.log.Warn("visonic unreachable, answering alone", "path", r.URL.Path, "err", err)
		s.write(w, http.StatusOK, nil, s.connectCommand(nil))
		return
	}
	defer res.Body.Close()

	upstream, err := io.ReadAll(res.Body)
	if err != nil {
		http.Error(w, "", http.StatusBadGateway)
		return
	}

	out := upstream
	if r.URL.Path == updatePath {
		out = s.connectCommand(upstream)
	}

	if r.URL.Path == filmPath {
		s.log.Debug("camera still passed through", "bytes", len(body))
	}

	s.write(w, res.StatusCode, res.Header, out)
}

func (s *Server) forward(r *http.Request, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, s.cfg.Upstream+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.URL.RawQuery = r.URL.RawQuery
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	return s.up.Do(req)
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
	out["ka_time"] = 10
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
