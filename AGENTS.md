# AGENTS.md

Guidance for AI coding agents working with this repository (arib).

## Credit

If you build something based on this repository, the author would appreciate a line like
this somewhere in it (for example, the README):

```
Based on symysak/arib — https://github.com/symysak/arib
```

## Legal

See `LEGAL.md`. Do not add features that publish or share received content, keep the web monitor on
`127.0.0.1` by default, and keep the one-time consent prompt (`internal/notice`, `-accept-notice`).

## License

- (c) 2026 symysak. No license is granted beyond that notice.
- `third_party/` contains third-party distributions under their own terms. Notices for those, and
  for Go and `github.com/coder/websocket` (both compiled into the binaries), are in
  `THIRD_PARTY_NOTICES.md`.

## Build

```sh
go build ./cmd/std-t86
go build ./cmd/std-t115/qpsknarrow
go build ./cmd/std-t79
go build ./cmd/std-t61
go build ./cmd/std-t102
go build ./cmd/arib      # all-in-one: `arib live --std <standard>` and `arib probe` (tries every standard at once)
bash scripts/std-t86/build_g7221.sh        # audio decoder for std-t86 (G.722.1)
bash scripts/std-t115/build_amrwbplus.sh   # audio decoder for std-t115-qpsknarrow (AMR-WB+)
bash scripts/std-t61/build_ambe2c.sh       # audio decoder for std-t79, std-t61 and std-t102 (AMBE+ 4000/2400, AMBE+ 2000, AMBE+2 3600x2450; ambe2-c; Windows: build_ambe2c.ps1)
```
