# Changelog

## 0.1.6

- The panel check-in interval is now an option, `ka_time`. It defaults to 10,
  which is what the original sends.

## 0.1.5

- Fixed camera uploads being cut off after two seconds. Go's client timeout
  covers the whole exchange where the Python's covers connecting and gaps
  between reads, so setting it to two seconds to match aborted large image
  uploads part way through.
- A failed upload now returns an error the panel can retry, rather than being
  answered with the check-in's connect command.

## 0.1.4

- Download mode now follows the panel as well as Home Assistant's requests, so
  the status Home Assistant reads stays correct when the panel enters or leaves
  download mode on its own.

## 0.1.3

- Added the HTTPS check-in server on port 8443. The reply to
  `/scripts/update.php` is what tells the panel to open its message connection,
  so without this the panel never connected at all.
- The certificate for it is generated on first start and kept in `/data/certs`.
- Camera stills are forwarded to Visonic.
- Stop messages from Home Assistant are acknowledged but no longer passed to the
  panel, which is what the original does.

## 0.1.2

- Every frame is logged in and out at debug level as spaced hex, so a line can
  be compared against the original's log.
- Anything the proxy does not understand is logged with the word `unsupported`.

## 0.1.1

- Fixed the acknowledgement gate never being released. Nothing told a connection
  that its peer had answered, so every message to the panel waited the full five
  seconds before the next could be sent. Panel data downloads timed out as a
  result and Home Assistant fell back to standard mode.

## 0.1.0

First release. Panel, Visonic and Home Assistant connections, the PowerLink 3.1
frame codec, routing, keepalive and watchdog, stealth mode, and packaging as a
Home Assistant app.
