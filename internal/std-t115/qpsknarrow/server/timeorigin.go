package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)


var (
	reSDRSharp = regexp.MustCompile(`(\d{2})-(\d{2})-(\d{2})_(\d{2})-(\d{2})-(\d{4})`)
	reTimeOnly = regexp.MustCompile(`(?:^|[^0-9])(\d{2})-(\d{2})-(\d{2})(?:[^0-9]|$)`)
)

func TimeOrigin(path string) (t0 time.Time, estimated bool) {
	base := filepath.Base(path)
	loc := time.Local

	if m := reSDRSharp.FindStringSubmatch(base); m != nil {
		hh, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		ss, _ := strconv.Atoi(m[3])
		dd, _ := strconv.Atoi(m[4])
		mo, _ := strconv.Atoi(m[5])
		yy, _ := strconv.Atoi(m[6])
		if validHMS(hh, mm, ss) && dd >= 1 && dd <= 31 && mo >= 1 && mo <= 12 && yy > 1970 {
			return time.Date(yy, time.Month(mo), dd, hh, mm, ss, 0, loc), false
		}
	}
	if m := reTimeOnly.FindStringSubmatch(base); m != nil {
		hh, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		ss, _ := strconv.Atoi(m[3])
		if validHMS(hh, mm, ss) {
			day := time.Now()
			if st, err := os.Stat(path); err == nil {
				day = st.ModTime()
			}
			return time.Date(day.Year(), day.Month(), day.Day(), hh, mm, ss, 0, loc), false
		}
	}
	if st, err := os.Stat(path); err == nil {
		return st.ModTime(), true
	}
	return time.Now(), true
}

func validHMS(h, m, s int) bool {
	return h >= 0 && h < 24 && m >= 0 && m < 60 && s >= 0 && s < 60
}
