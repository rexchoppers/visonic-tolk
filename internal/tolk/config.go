package tolk

import (
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"
)

// Defaults are the python's, from const.py.
const (
	defaultVisonicHost = "52.58.105.181"
	defaultMessagePort = "5001"
	defaultMonitorPort = "5002"
	defaultReconnect   = 10 * time.Second
	defaultKeepalive   = 32 * time.Second
	defaultWatchdog    = 120 * time.Second
)

// FromEnv reads the same names const.py uses, so an addon script that exports
// its options reaches this without a translation layer.
func FromEnv() Config {
	host := env("VISONIC_HOST", defaultVisonicHost)
	port := env("MESSAGE_PORT", defaultMessagePort)

	return Config{
		PanelAddr:   ":" + port,
		MonitorAddr: ":" + env("ALARM_MONITOR_PORT", defaultMonitorPort),
		VisonicAddr: net.JoinHostPort(host, port),
		Reconnect:   seconds("VISONIC_RECONNECT_INTERVAL", defaultReconnect),
		Keepalive:   seconds("KEEPALIVE_TIMER", defaultKeepalive),
		Watchdog:    seconds("WATCHDOG_TIMEOUT", defaultWatchdog),
	}
}

// LogLevel reads LOG_LEVEL, falling back to info on anything unrecognised.
func LogLevel() slog.Level {
	switch env("LOG_LEVEL", "info") {
	case "debug":
		return slog.LevelDebug
	case "warning":
		return slog.LevelWarn
	case "error", "critical":
		return slog.LevelError
	}
	return slog.LevelInfo
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func seconds(name string, fallback time.Duration) time.Duration {
	v, err := strconv.Atoi(os.Getenv(name))
	if err != nil || v <= 0 {
		return fallback
	}
	return time.Duration(v) * time.Second
}
