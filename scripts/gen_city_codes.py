from __future__ import annotations

import csv
import io
import re
import sys
import time
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
import zipfile
from pathlib import Path

OUT_DIR = Path(__file__).resolve().parent.parent / "internal" / "citycodes"
ESTAT_BASE = "https://www.e-stat.go.jp/municipalities/cities/areacode"


def _fetch(page: int) -> str:
    url = ESTAT_BASE if page == 1 else f"{ESTAT_BASE}?page={page}"
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    return urllib.request.urlopen(req, timeout=30).read().decode("utf-8", "replace")


def _parse_page(html: str) -> tuple[list[str], list[list[str]]]:
    tables = re.findall(
        r'<table class="stat-inspect-table[^"]*"[^>]*>(.*?)</table>', html, re.S)
    codes: list[str] = []
    rows: list[list[str]] = []
    for t in tables:
        for tr in re.findall(r"<tr[^>]*>(.*?)</tr>", t, re.S):
            cells = [re.sub(r"\s+", " ", re.sub(r"<[^>]+>", " ", c)).strip()
                     for c in re.findall(r"<td[^>]*>(.*?)</td>", tr, re.S)]
            if not cells:
                continue
            if len(cells) == 1 and re.fullmatch(r"\d{5}", cells[0]):
                codes.append(cells[0])
            elif len(cells) >= 4:
                rows.append(cells)
    return codes, rows


def fetch_estat() -> dict[int, tuple[str, str, str]]:
    out: dict[int, tuple[str, str, str]] = {}
    page, stale = 1, 0
    while page <= 200 and stale < 2:
        codes, rows = _parse_page(_fetch(page))
        if not codes:
            break
        if len(codes) != len(rows):
            raise RuntimeError(f"page {page}: コード{len(codes)}件と名称{len(rows)}件が不一致")
        before = len(out)
        for code, r in zip(codes, rows, strict=True):
            out[int(code)] = (r[0], r[1], r[3])
        stale = stale + 1 if len(out) == before else 0
        print(f"  page {page}: {len(out)} 件", file=sys.stderr)
        page += 1
        time.sleep(0.2)
    return out


def build_names(sac: dict[int, tuple[str, str, str]]) -> dict[int, str]:
    out: dict[int, str] = {}
    for code, (pref, parent, mun) in sac.items():
        parts = [pref]
        if parent and parent != mun and not parent.endswith(("支庁", "振興局")):
            parts.append(parent)
        if mun:
            parts.append(mun)
        out[code] = " ".join(parts)
    return out


MERGER_PAGE = ("https://www.e-stat.go.jp/municipalities/cities/"
               "absorption-separation-of-municipalities")


def _form_query(html: str) -> list[tuple[str, str]]:
    form = html[html.index('data-alias="main_form"'):]
    q: list[tuple[str, str]] = []
    for m in re.finditer(r"<input[^>]*>", form):
        tag = m.group(0)
        name = re.search(r'name="([^"]+)"', tag)
        if not name:
            continue
        typ = (re.search(r'type="([^"]+)"', tag) or [None, "text"])[1]
        val = re.search(r'value="([^"]*)"', tag)
        if typ == "checkbox":
            if "checked" in tag:
                q.append((name.group(1), val.group(1) if val else "on"))
        elif typ in ("text", "hidden"):
            q.append((name.group(1), val.group(1) if val else ""))
    for m in re.finditer(r'<select([^>]*)name="([^"]+)"[^>]*>(.*?)</select>', form, re.S):
        attrs, name, body = m.group(1), m.group(2), m.group(3)
        sels = re.findall(r'<option[^>]*value="([^"]*)"[^>]*selected', body)
        if "multiple" in attrs or name.endswith("[]"):
            q += [(name, v) for v in sels]
        else:
            first = re.search(r'<option[^>]*value="([^"]*)"', body)
            q.append((name, sels[0] if sels else (first.group(1) if first else "")))
    return q


def fetch_mergers() -> list[dict[str, str]]:
    req = urllib.request.Request(MERGER_PAGE, headers={"User-Agent": "Mozilla/5.0"})
    html = urllib.request.urlopen(req, timeout=60).read().decode("utf-8", "replace")
    q = _form_query(html) + [("file_format", "csv"), ("charset", "Shift-JIS"),
                             ("bom", ""), ("op", "download")]
    url = MERGER_PAGE + "?" + urllib.parse.urlencode(q)
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    raw = urllib.request.urlopen(req, timeout=180).read()
    if raw[:200].lstrip().startswith(b"<"):
        raise RuntimeError("CSV ではなく HTML が返った（フォーム項目が足りない？）")
    text = raw.decode("cp932", "replace")
    return list(csv.DictReader(io.StringIO(text)))


def fetch_abolished(current: dict[int, str]) -> dict[int, tuple[str, str, str]]:
    out: dict[int, tuple[str, str, str]] = {}
    for row in fetch_mergers():
        code_s = (row.get("標準地域コード") or "").strip()
        if not re.fullmatch(r"\d{5}", code_s):
            continue
        code = int(code_s)
        if code in current:
            continue
        pref = (row.get("都道府県") or "").strip()
        parent = (row.get("政令市･郡･支庁･振興局等") or "").strip()
        mun = (row.get("市区町村") or "").strip()
        parts = [pref]
        if parent and parent != mun and not parent.endswith(("支庁", "振興局")):
            parts.append(parent)
        if mun:
            parts.append(mun)
        name = " ".join(p for p in parts if p)
        date = (row.get("廃置分合等施行年月日") or "").strip()
        reason = re.sub(r"\s+", " ", (row.get("改正事由") or "").strip())
        if code not in out or date > out[code][1]:
            out[code] = (name, date, reason)
    return out


_NS = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"


def _col(ref: str) -> int:
    letters = re.match(r"([A-Z]+)", ref).group(1)
    n = 0
    for ch in letters:
        n = n * 26 + (ord(ch) - 64)
    return n - 1


def _shared_strings(z: zipfile.ZipFile) -> list[str]:
    root = ET.fromstring(z.read("xl/sharedStrings.xml"))
    out = []
    for si in root.findall(_NS + "si"):
        parts = []
        for child in si:
            if child.tag == _NS + "t":
                parts.append(child.text or "")
            elif child.tag == _NS + "r":
                for t in child.findall(_NS + "t"):
                    parts.append(t.text or "")
        out.append("".join(parts))
    return out


def read_mic_codes(xlsx: Path) -> set[int]:
    z = zipfile.ZipFile(xlsx)
    shared = _shared_strings(z)
    codes: set[int] = set()
    for sheet in sorted(n for n in z.namelist() if n.startswith("xl/worksheets/sheet")):
        root = ET.fromstring(z.read(sheet))
        for row in root.iter(_NS + "row"):
            cells: dict[int, str] = {}
            for c in row.findall(_NS + "c"):
                v = c.find(_NS + "v")
                if v is None:
                    continue
                val = shared[int(v.text)] if c.get("t") == "s" else (v.text or "")
                cells[_col(c.get("r"))] = val
            code6 = (cells.get(0) or "").strip()
            mun = (cells.get(2) or "").replace("\n", "").strip()
            if re.fullmatch(r"\d{6}", code6) and mun:
                codes.add(int(code6[:5]))
    return codes


def _clean(s: str) -> str:
    return s.replace("\t", " ").replace("\n", " ").replace("\r", " ").strip()


def write_tsv(mapping: dict[int, str],
              abolished: dict[int, tuple[str, str, str]]) -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    (OUT_DIR / "current.tsv").write_text(
        "".join(f"{c}\t{_clean(n)}\n" for c, n in sorted(mapping.items())),
        encoding="utf-8")
    (OUT_DIR / "abolished.tsv").write_text(
        "".join(f"{c}\t{_clean(name)}\t{_clean(date)}\t{_clean(reason)}\n"
                for c, (name, date, reason) in sorted(abolished.items())),
        encoding="utf-8")


def read_existing_abolished() -> dict[int, tuple[str, str, str]]:
    p = OUT_DIR / "abolished.tsv"
    if not p.exists():
        return {}
    out: dict[int, tuple[str, str, str]] = {}
    for ln in p.read_text(encoding="utf-8").splitlines():
        if not ln.strip():
            continue
        f = ln.split("\t")
        out[int(f[0])] = (f[1], f[2], f[3] if len(f) > 3 else "")
    return out


def main(argv: list[str]) -> int:
    args = [a for a in argv[1:] if not a.startswith("--")]
    with_abolished = "--no-abolished" not in argv
    print("e-Stat 標準地域コードを取得中…", file=sys.stderr)
    sac = fetch_estat()
    mapping = build_names(sac)
    if args:
        mic = read_mic_codes(Path(args[0]))
        only_estat = sorted(set(mapping) - mic)
        only_mic = sorted(mic - set(mapping))
        if only_estat or only_mic:
            print(f"警告: コード集合が不一致 e-Statのみ={only_estat[:10]} "
                  f"総務省のみ={only_mic[:10]}", file=sys.stderr)
        else:
            print(f"クロスチェック OK: {len(mic)} 件一致", file=sys.stderr)
    abolished: dict[int, tuple[str, str, str]] = {}
    if with_abolished:
        print("廃置分合等情報（CSV 全件）から廃止コードを収集中…", file=sys.stderr)
        abolished = fetch_abolished(mapping)
    else:
        abolished = read_existing_abolished()
        print(f"廃止コードは既存の {len(abolished)} 件を保持", file=sys.stderr)
    write_tsv(mapping, abolished)
    print(f"{OUT_DIR}: 現行 {len(mapping)} 件 / 廃止 {len(abolished)} 件を書き出しました。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
