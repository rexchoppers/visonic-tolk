package webtest

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/web"
)

func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// serve starts the web side against a stand-in for Visonic.
func serve(t *testing.T, upstream string) string {
	t.Helper()

	addr := freeAddr(t)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go web.New(web.Config{
		Addr:        addr,
		Upstream:    upstream,
		ConnectPort: "5001",
		CertDir:     t.TempDir(),
	}, slog.New(slog.DiscardHandler)).Serve(ctx)

	return addr
}

// waitFor blocks until the server is accepting, so the post itself can be a
// single attempt. Retrying a post would retry the upstream timeout with it.
func waitFor(t *testing.T, addr string) {
	t.Helper()

	for range 100 {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never started accepting", addr)
}

func post(t *testing.T, addr, path, body string) (int, []byte) {
	t.Helper()

	waitFor(t, addr)

	c := &http.Client{
		Timeout:   20 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}

	res, err := c.Post("https://"+addr+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()

	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return res.StatusCode, out
}

// The whole reason this package exists: without the connect command in this
// reply, the panel never opens its message connection.
func TestTheCheckInTellsThePanelToConnect(t *testing.T) {
	visonic := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ka_time":5,"cmds":[]}`))
	}))
	defer visonic.Close()

	status, body := post(t, serve(t, visonic.URL), "/scripts/update.php", "{}")

	if status != http.StatusOK {
		t.Fatalf("status %d, want 200", status)
	}

	var out struct {
		Cmds []struct {
			Name   string         `json:"name"`
			Params map[string]any `json:"params"`
		} `json:"cmds"`
		KaTime  int `json:"ka_time"`
		Version int `json:"version"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("reply is not json: %v, got %q", err, body)
	}

	if len(out.Cmds) != 1 || out.Cmds[0].Name != "connect" {
		t.Fatalf("reply carried no connect command: %q", body)
	}
	if got := out.Cmds[0].Params["port"]; got != float64(5001) {
		t.Errorf("told the panel port %v, want 5001", got)
	}
	if out.KaTime != 10 || out.Version != 3 {
		t.Errorf("ka_time %d version %d, want 10 and 3", out.KaTime, out.Version)
	}
}

func TestThePanelIsStillToldToConnectWithVisonicDown(t *testing.T) {
	_, body := post(t, serve(t, "https://192.0.2.1:8443"), "/scripts/update.php", "{}")

	if !strings.Contains(string(body), `"connect"`) {
		t.Errorf("no connect command with visonic unreachable: %q", body)
	}
}

func TestOtherPathsArePassedThrough(t *testing.T) {
	visonic := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("raw bytes, not json"))
	}))
	defer visonic.Close()

	_, body := post(t, serve(t, visonic.URL), "/scripts/pir_film.php", "image")

	if string(body) != "raw bytes, not json" {
		t.Errorf("got %q, want it untouched", body)
	}
}

func TestTheRequestReachesVisonicIntact(t *testing.T) {
	seen := make(chan string, 1)

	visonic := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen <- r.URL.Path + "?" + r.URL.RawQuery + "|" + string(b)
		w.Write([]byte("{}"))
	}))
	defer visonic.Close()

	post(t, serve(t, visonic.URL), "/scripts/update.php?id=7", "hello")

	select {
	case got := <-seen:
		if got != "/scripts/update.php?id=7|hello" {
			t.Errorf("visonic saw %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("visonic never saw the request")
	}
}
