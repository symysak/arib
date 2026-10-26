package notice

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/symysak/arib/internal/credit"
)

const EnvAccept = "ARIB_ACCEPT_NOTICE"

var Text = `特定の相手方に向けた無線通信の存在や内容を漏らすこと・窃用することは、電波法 第 59 条で禁じられています
（罰則は第 109 条。暗号通信の内容を漏らす・窃用する目的で復元することは第 109 条の 2）。

  - 受信したものを SNS・配信・掲示板などに載せない
  - Web モニタを LAN やインターネットへ開かない
  - 録音や記録を人に渡さない

関係法令に従って使ってください。その判断と結果の責任は利用者にあります。
詳しくは LEGAL.md（` + credit.URL + `/blob/main/LEGAL.md）。
`

func Ensure(accept bool) error {
	if os.Getenv(EnvAccept) == "1" {
		return nil
	}
	path := ""
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		path = filepath.Join(filepath.Dir(exe), "notice-accepted")
	}
	return ensure(path, accept, isTerminal(os.Stdin), os.Stdin, os.Stderr)
}

func ensure(path string, accept, tty bool, in io.Reader, out io.Writer) error {
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	if !accept {
		fmt.Fprintf(out, "\n%s\n", Text)
		if !tty {
			return errors.New("この注意に同意するなら -accept-notice を付けて起動し直してください（同意は一度だけで、次からは聞きません）")
		}
		fmt.Fprint(out, "同意するなら yes と入力してください: ")
		line, _ := bufio.NewReader(in).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "yes" {
			return errors.New("同意が無いので受信を始めません（端末で聞けないところから起動するなら -accept-notice を付けてください）")
		}
	}
	if path == "" {
		fmt.Fprintln(out, "注意: 同意を記録する場所が分からないので、次の起動でもまた聞きます")
		return nil
	}
	if err := record(path); err != nil {
		fmt.Fprintf(out, "注意: 同意を記録できなかったので、次の起動でもまた聞きます（%v）\n", err)
		return nil
	}
	fmt.Fprintf(out, "同意を記録しました（%s。消すとまた聞きます）\n", path)
	return nil
}

func record(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
