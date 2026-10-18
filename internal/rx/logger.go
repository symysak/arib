package rx

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Logger struct {
	mu  sync.Mutex
	f   *os.File
	out io.Writer
}

func NewLogger(path string, out io.Writer) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Logger{f: f, out: out}, nil
}

func (l *Logger) Printf(format string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := time.Now().Format("2006-01-02 15:04:05 ") + fmt.Sprintf(format, a...)
	fmt.Fprintln(l.f, line)
	if l.out != nil {
		fmt.Fprintln(l.out, line)
	}
}

func (l *Logger) Close() { l.f.Close() }
