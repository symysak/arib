package credit

import (
	"fmt"
	"runtime/debug"
)

const (
	Project = "arib"
	Author  = "symysak"
	URL     = "https://github.com/symysak/arib"
	Copyright = "(c) 2026 symysak"
	Generator = "symysak/arib"
	Attribution = "Based on symysak/arib — https://github.com/symysak/arib"
)

func Version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "devel"
}

func Line() string {
	return fmt.Sprintf("%s — %s", Generator, URL)
}

func Text(bin string) string {
	return fmt.Sprintf("%s %s\n%s — %s\n%s\n", bin, Version(), Generator, Copyright, URL)
}
