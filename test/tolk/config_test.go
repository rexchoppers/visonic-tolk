package tolktest

import (
	"log/slog"
	"testing"
	"time"

	"github.com/rexchoppers/visonic-tolk/internal/tolk"
)

func TestFromEnvDefaultsToThePythonsValues(t *testing.T) {
	cfg := tolk.FromEnv()

	want := tolk.Config{
		PanelAddr:   ":5001",
		MonitorAddr: ":5002",
		VisonicAddr: "52.58.105.181:5001",
		Reconnect:   10 * time.Second,
		Keepalive:   32 * time.Second,
		Watchdog:    120 * time.Second,

		StealthTimeout: 10 * time.Second,
		WebAddr:        ":8443",
		WebUpstream:    "https://52.58.105.181:8443",
		CertDir:        "/data/certs",
	}
	if cfg != want {
		t.Errorf("\n got %+v\nwant %+v", cfg, want)
	}
}

func TestFromEnvReadsTheEnvironment(t *testing.T) {
	t.Setenv("VISONIC_HOST", "panel.example")
	t.Setenv("MESSAGE_PORT", "6001")
	t.Setenv("ALARM_MONITOR_PORT", "6002")
	t.Setenv("VISONIC_RECONNECT_INTERVAL", "3")
	t.Setenv("KEEPALIVE_TIMER", "7")
	t.Setenv("WATCHDOG_TIMEOUT", "90")
	t.Setenv("STEALTH_MODE_TIMEOUT", "5")
	t.Setenv("WEBSERVER_PORT", "9443")
	t.Setenv("SSL_CERT_PATH", "/tmp/certs")

	cfg := tolk.FromEnv()

	want := tolk.Config{
		PanelAddr:   ":6001",
		MonitorAddr: ":6002",
		VisonicAddr: "panel.example:6001",
		Reconnect:   3 * time.Second,
		Keepalive:   7 * time.Second,
		Watchdog:    90 * time.Second,

		StealthTimeout: 5 * time.Second,
		WebAddr:        ":9443",
		WebUpstream:    "https://panel.example:9443",
		CertDir:        "/tmp/certs",
	}
	if cfg != want {
		t.Errorf("\n got %+v\nwant %+v", cfg, want)
	}
}

func TestReconnectIgnoresRubbish(t *testing.T) {
	for _, v := range []string{"", "nonsense", "0", "-5"} {
		t.Setenv("VISONIC_RECONNECT_INTERVAL", v)

		if got := tolk.FromEnv().Reconnect; got != 10*time.Second {
			t.Errorf("%q gave %v, want the 10s default", v, got)
		}
	}
}

func TestLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":    slog.LevelDebug,
		"info":     slog.LevelInfo,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"critical": slog.LevelError,
		"":         slog.LevelInfo,
		"nonsense": slog.LevelInfo,
	}

	for in, want := range cases {
		t.Setenv("LOG_LEVEL", in)

		if got := tolk.LogLevel(); got != want {
			t.Errorf("%q gave %v, want %v", in, got, want)
		}
	}
}
