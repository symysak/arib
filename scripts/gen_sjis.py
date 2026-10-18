from pathlib import Path
import struct

LEADS = list(range(0x81, 0xA0)) + list(range(0xE0, 0xFD))
TRAILS = range(0x40, 0xFD)


def main() -> None:
    out = bytearray()
    n = 0
    for a in LEADS:
        for b in TRAILS:
            try:
                s = bytes([a, b]).decode("cp932")
            except UnicodeDecodeError:
                s = ""
            cp = ord(s) if len(s) == 1 else 0
            assert cp <= 0xFFFF
            n += cp != 0
            out += struct.pack("<H", cp)
    dst = Path(__file__).resolve().parent.parent / "internal" / "sjis" / "cp932.bin"
    dst.write_bytes(bytes(out))
    print(f"{dst}: {len(LEADS)}×{len(TRAILS)} 枠、定義 {n} 文字")


if __name__ == "__main__":
    main()
