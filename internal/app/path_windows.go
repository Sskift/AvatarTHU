package app

import (
	"fmt"
	"golang.org/x/sys/windows/registry"
	"strings"
)

func installUserPath(bin string) {
	key, _, e := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	check(e)
	defer key.Close()
	value, _, e := key.GetStringValue("Path")
	if e != nil && e != registry.ErrNotExist {
		check(e)
	}
	for _, p := range strings.Split(value, ";") {
		if strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(bin, `\`)) {
			return
		}
	}
	if value != "" && !strings.HasSuffix(value, ";") {
		value += ";"
	}
	check(key.SetExpandStringValue("Path", value+bin))
	fmt.Println("命令路径已加入当前用户 PATH；重新登录或在新终端刷新 PATH 后直接使用 avatarthu。")
}
