package webui

import (
	"io/fs"
	"path"
	"strings"
)

var absolutePatterns = []string{
	"location.host",
	`"/api/`, `'/api/`, "`/api/",
	`"/ws`, `'/ws`, "`/ws",
	`href="/`, `src="/`, `href='/`, `src='/`,
	`fetch("/`, `fetch('/`, "fetch(`/",
	`url(/`, `url("/`, `url('/`,
}

func AbsoluteRefs(fsys fs.FS) ([]string, error) {
	var bad []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		switch path.Ext(p) {
		case ".js", ".html", ".css":
		default:
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		src := string(b)
		for _, pat := range absolutePatterns {
			for i := strings.Index(src, pat); i >= 0; {
				end := i + len(pat)
				if !(strings.HasSuffix(pat, "/") && end < len(src) && src[end] == '/') {
					bad = append(bad, p+": "+pat)
					break
				}
				next := strings.Index(src[end:], pat)
				if next < 0 {
					break
				}
				i = end + next
			}
		}
		return nil
	})
	return bad, err
}
