package webtest

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A camera upload is large and Visonic is not quick about it. The whole
// exchange must not be cut off by a deadline sized for the check-in.
func TestASlowCameraUploadIsNotCutOff(t *testing.T) {
	visonic := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		time.Sleep(4 * time.Second)
		w.Write([]byte("stored " + string(rune('0'+len(body)%10))))
	}))
	defer visonic.Close()

	status, body := post(t, serve(t, visonic.URL), "/scripts/pir_film.php", string(bytes.Repeat([]byte("x"), 200_000)))

	if status != http.StatusOK {
		t.Fatalf("status %d, want 200, so the upload was cut short", status)
	}
	if !bytes.HasPrefix(body, []byte("stored")) {
		t.Errorf("got %q, want visonic's own answer", body)
	}
}

// A failed camera upload must not be answered with the check-in's reply.
func TestAFailedCameraUploadIsNotGivenAConnectCommand(t *testing.T) {
	status, body := post(t, serve(t, "https://192.0.2.1:8443"), "/scripts/pir_film.php", "image")

	if bytes.Contains(body, []byte("connect")) {
		t.Errorf("camera upload answered with a connect command: %q", body)
	}
	if status == http.StatusOK {
		t.Errorf("status %d, want a failure the panel can retry", status)
	}
}
