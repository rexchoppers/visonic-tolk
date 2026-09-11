# visonic-tolk

- No comments in code unless absolutely necessary.
- Function names short and plain.
- Use a library rather than reinventing. Standard library first, then a well used module.
- Claude never commits and never pushes. The human commits.
- Claude generates code. The human reviews every line before it lands.
- Nothing is done until it has been physically tested against the real panel.

## Scope

- Socket mode only. The alarm monitor is a raw TCP server on port 5002.
- Websocket mode is out of scope. Nothing listens on 8082.
- So the full B0 decoder, the standard message decoder and the lookup tables
  are not ported. Only the command byte is read, for routing.
- tolk connects out to the Visonic cloud. That is the point of the addon, and
  the old python had no switch to turn it off.

## Commits

- `<type>(<scope>): <subject>`, scope optional.
- Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`.
- Imperative, lowercase, no full stop, 72 characters at most.
- No Claude attribution in commits.
