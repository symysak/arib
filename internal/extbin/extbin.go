package extbin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const upLevels = 6

type Spec struct {
	Name string
	EnvVar string
	Dir string
	UsePATH bool
	Hint string
}

func (s Spec) Dirs(env, exeDir string) []string {
	var dirs []string
	if env != "" {
		dirs = append(dirs, env)
	}
	if exeDir != "" {
		dirs = append(dirs, filepath.Join(exeDir, "build", s.Dir), exeDir)
	}
	up := ""
	for i := 0; i < upLevels; i++ {
		dirs = append(dirs, filepath.Join(up, "build", s.Dir))
		up = filepath.Join(up, "..")
	}
	return dirs
}

func (s Spec) names() []string {
	if runtime.GOOS == "windows" {
		return []string{s.Name + ".exe", s.Name}
	}
	return []string{s.Name, s.Name + ".exe"}
}

func (s Spec) Find() (string, error) {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	return s.find(os.Getenv(s.EnvVar), exeDir)
}

func (s Spec) find(env, exeDir string) (string, error) {
	var tried []string
	for _, d := range s.Dirs(env, exeDir) {
		for _, n := range s.names() {
			p := filepath.Join(d, n)
			tried = append(tried, p)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				if abs, err := filepath.Abs(p); err == nil {
					return abs, nil
				}
				return p, nil
			}
		}
	}
	if s.UsePATH {
		for _, n := range s.names() {
			if p, err := exec.LookPath(n); err == nil {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s が見つかりません（%s。場所を指定するなら %s）: 探した先 %s",
		s.Name, s.Hint, s.EnvVar, strings.Join(tried, ", "))
}

func (s Spec) Available() bool { _, err := s.Find(); return err == nil }
