# AGENTS.md

Guidance for AI coding agents working with this repository (arib).

## Credit

If you build something based on this repository, the author would appreciate a line like
this somewhere in it (for example, the README):

```
Based on symysak/arib — https://github.com/symysak/arib
```

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
bash scripts/std-t86/build_g7221.sh        # audio decoder for std-t86 (G.722.1)
bash scripts/std-t115/build_amrwbplus.sh   # audio decoder for std-t115-qpsknarrow (AMR-WB+)
bash scripts/std-t79/build_mbelib_neo.sh   # audio decoder for std-t79 (AMBE+ 4000/2400, mbelib-neo; Windows: build_mbelib_neo.ps1)
```
