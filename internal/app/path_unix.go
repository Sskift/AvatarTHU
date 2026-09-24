//go:build !windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func installUserPath(bin string) {
	if strings.Contains(":"+os.Getenv("PATH")+":", ":"+bin+":") {
		return
	}
	home, e := os.UserHomeDir()
	check(e)
	file := ".profile"
	if strings.HasSuffix(os.Getenv("SHELL"), "zsh") {
		file = ".zshrc"
	} else if strings.HasSuffix(os.Getenv("SHELL"), "bash") {
		file = ".bashrc"
	}
	p := filepath.Join(home, file)
	line := `export PATH="$HOME/.local/bin:$PATH"`
	b, e := os.ReadFile(p)
	if e != nil && !os.IsNotExist(e) {
		check(e)
	}
	if !strings.Contains(string(b), line) {
		f, e := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		check(e)
		_, e = f.WriteString("\n# AvatarTHU command\n" + line + "\n")
		check(e)
		check(f.Close())
	}
	fmt.Println("命令路径已加入 " + p + "；新终端可直接使用 avatarthu。")
}
