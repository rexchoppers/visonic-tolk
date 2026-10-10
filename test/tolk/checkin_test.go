package tolktest

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/tolk"
)

// checkIn posts the panel's check-in to tolk's web side and returns the reply.
func checkIn(t *testing.T, addr string) string {
	t.Helper()

	c := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}

	for range 100 {
		res, err := c.Post("https://"+addr+"/scripts/update.php", "application/json", strings.NewReader("{}"))
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		defer res.Body.Close()

		b, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return string(b)
	}

	t.Fatalf("%s never answered a check-in", addr)
	return ""
}

func startWeb(t *testing.T) tolk.Config {
	t.Helper()

	visonic := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ka_time":5}`))
	}))
	t.Cleanup(visonic.Close)

	return startWith(t, tolk.Config{
		VisonicAddr: "192.0.2.1:5001",
		Reconnect:   time.Hour,
		WebAddr:     freeAddr(t),
		WebUpstream: visonic.URL,
		CertDir:     t.TempDir(),
	})
}

func TestAPanelWithNoConnectionIsToldToConnect(t *testing.T) {
	cfg := startWeb(t)

	if reply := checkIn(t, cfg.WebAddr); !strings.Contains(reply, `"connect"`) {
		t.Errorf("no connect command with no panel connected: %q", reply)
	}
}

func TestAConnectedPanelIsNotToldToConnectAgain(t *testing.T) {
	cfg := startWeb(t)

	pan := dial(t, cfg.PanelAddr)
	pan.Write(frame(1, []byte{0x0d, 0x06, 0xf9, 0x0a}).Encode())
	time.Sleep(100 * time.Millisecond)

	if reply := checkIn(t, cfg.WebAddr); reply != `{"ka_time":5}` {
		t.Errorf("got %q, want visonic's reply untouched", reply)
	}
}
