package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)


type timeOrigin struct {
	WallMS    int64
	Estimated bool
	Source    string
}

var (
	reStampDateTime = regexp.MustCompile(`(?:^|[^0-9])(\d{8})-(\d{2})(\d{2})(\d{2})(?:[^0-9]|$)`)
	reStampTimeOnly = regexp.MustCompile(`^(\d{2})-(\d{2})-(\d{2})(?:[^0-9]|$)`)
)

func fileTimeOrigin(path string) timeOrigin {
	mtime, haveMtime := fileModTime(path)
	base := mtime
	if !haveMtime {
		base = time.Now()
	}
	name := filepath.Base(path)

	if m := reStampDateTime.FindStringSubmatch(name); m != nil {
		y, _ := strconv.Atoi(m[1][:4])
		mo, _ := strconv.Atoi(m[1][4:6])
		d, _ := strconv.Atoi(m[1][6:8])
		hh, mm, ss := atoi3(m[2], m[3], m[4])
		if validHMS(hh, mm, ss) && mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			t := time.Date(y, time.Month(mo), d, hh, mm, ss, 0, base.Location())
			return timeOrigin{WallMS: t.UnixMilli(), Source: "ファイル名の受信時刻"}
		}
	}

	if m := reStampTimeOnly.FindStringSubmatch(name); m != nil {
		hh, mm, ss := atoi3(m[1], m[2], m[3])
		if validHMS(hh, mm, ss) {
			t := time.Date(base.Year(), base.Month(), base.Day(), hh, mm, ss, 0, base.Location())
			if t.After(base) {
				t = t.AddDate(0, 0, -1)
			}
			src := "ファイル名の受信時刻"
			if !haveMtime {
				src += "（日付は不明のため本日と仮定）"
			} else {
				src += "（日付はファイル更新時刻から）"
			}
			return timeOrigin{WallMS: t.UnixMilli(), Source: src}
		}
	}

	if haveMtime {
		return timeOrigin{
			WallMS:    mtime.UnixMilli(),
			Estimated: true,
			Source:    "ファイル更新時刻（推定）",
		}
	}
	return timeOrigin{
		WallMS:    time.Now().UnixMilli(),
		Estimated: true,
		Source:    "再生開始時刻（推定。ファイルから時刻を読めません）",
	}
}

func liveTimeOrigin() timeOrigin {
	return timeOrigin{WallMS: nowMS(), Source: "受信開始時刻"}
}

func fileModTime(path string) (time.Time, bool) {
	if path == "" {
		return time.Time{}, false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

func atoi3(a, b, c string) (int, int, int) {
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	z, _ := strconv.Atoi(c)
	return x, y, z
}

func validHMS(h, m, s int) bool {
	return h >= 0 && h < 24 && m >= 0 && m < 60 && s >= 0 && s < 60
}
