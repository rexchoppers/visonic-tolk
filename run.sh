#!/usr/bin/with-contenv bashio

# Exported, not just assigned. The app this replaces set these as shell
# variables, so the process never saw any of them and every option in the
# Home Assistant UI did nothing.
export VISONIC_HOST="$(bashio::config 'visonic_host')"
export MESSAGE_PORT="$(bashio::config 'message_port')"
export ALARM_MONITOR_PORT="$(bashio::config 'alarm_monitor_port')"
export VISONIC_RECONNECT_INTERVAL="$(bashio::config 'visonic_reconnect_interval')"
export KEEPALIVE_TIMER="$(bashio::config 'keepalive_timer')"
export WATCHDOG_TIMEOUT="$(bashio::config 'watchdog_timeout')"
export STEALTH_MODE_TIMEOUT="$(bashio::config 'stealth_mode_timeout')"
export LOG_LEVEL="$(bashio::config 'log_level')"

exec /usr/bin/tolk
