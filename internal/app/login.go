// Browser session import and renewal derived from AutoThu (MIT).
package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func parseMap(b []byte) M {
	var m M
	check(json.Unmarshal(b, &m))
	ensure(m != nil, "CLI 未返回 JSON 对象")
	return m
}
func (a *App) saveSession(cookies []M) {
	s := a.schoolFrom(M{"all_cookies": cookies})
	s.semester()
	s.courses()
	defer fileLock(a.session()+".lock", true)()
	writeJSON(a.session(), M{"all_cookies": s.Jar.snapshot()})
}
func (a *App) importChrome() []M {
	ensure(runtime.GOOS == "darwin", "Chrome 会话导入仅适用于 macOS；Windows 请使用登录窗口")
	home, e := os.UserHomeDir()
	check(e)
	base := filepath.Join(home, "Library/Application Support/Google/Chrome")
	state := readMap(filepath.Join(base, "Local State"))
	profile := strDefault(obj(state, "profile"), "last_used", "Default")
	db := filepath.Join(base, profile, "Cookies")
	ensure(exists(db), "找不到 Chrome 登录记录")
	out, _, e := capture(a.Ctx, a.Root, 20*time.Second, "/usr/bin/security", "find-generic-password", "-w", "-s", "Chrome Safe Storage", "-a", "Chrome")
	ensure(e == nil, "Chrome 钥匙串不可用，请使用登录窗口")
	key, e := pbkdf2.Key(sha1.New, strings.TrimRight(string(out), "\r\n"), []byte("saltysalt"), 1003, 16)
	check(e)
	q := `SELECT host_key,name,value,hex(encrypted_value) AS encrypted FROM cookies WHERE host_key IN ('learn.tsinghua.edu.cn','.learn.tsinghua.edu.cn') AND name IN ('JSESSIONID','XSRF-TOKEN') ORDER BY last_access_utc;`
	out, _, e = capture(a.Ctx, a.Root, 10*time.Second, "/usr/bin/sqlite3", "-readonly", "-json", db, q)
	ensure(e == nil, "无法读取 Chrome 登录记录，请使用登录窗口")
	var rows []M
	check(json.Unmarshal(out, &rows))
	versionBytes, _, e := capture(a.Ctx, a.Root, 10*time.Second, "/usr/bin/sqlite3", "-readonly", db, `SELECT value FROM meta WHERE key='version';`)
	check(e)
	version, _ := strconv.Atoi(strings.TrimSpace(string(versionBytes)))
	result := []M{}
	for _, r := range rows {
		v := str(r, "value")
		if encrypted := str(r, "encrypted"); encrypted != "" {
			b, e := hex.DecodeString(encrypted)
			check(e)
			ensure(bytes.HasPrefix(b, []byte("v10")), "Chrome 加密格式不支持，请使用登录窗口")
			b = b[3:]
			ensure(len(b) > 0 && len(b)%aes.BlockSize == 0, "Chrome Cookie 无效")
			block, e := aes.NewCipher(key)
			check(e)
			plain := make([]byte, len(b))
			cipher.NewCBCDecrypter(block, bytes.Repeat([]byte(" "), 16)).CryptBlocks(plain, b)
			pad := int(plain[len(plain)-1])
			ensure(pad > 0 && pad <= 16 && pad <= len(plain), "Chrome Cookie 无效")
			ensure(bytes.Equal(plain[len(plain)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)), "Chrome Cookie 无效")
			plain = plain[:len(plain)-pad]
			if version >= 24 {
				h := sha256.Sum256([]byte(str(r, "host_key")))
				ensure(len(plain) >= 32 && bytes.Equal(plain[:32], h[:]), "Chrome Cookie 域名校验失败")
				plain = plain[32:]
			}
			v = string(plain)
		}
		result = append(result, M{"name": r["name"], "value": v, "domain": r["host_key"], "path": "/"})
	}
	return result
}
func findChrome() string {
	paths := []string{os.Getenv("AVATARTHU_CHROME")}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		paths = append(paths, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"))
	case "windows":
		for _, p := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			paths = append(paths, filepath.Join(p, "Google/Chrome/Application/chrome.exe"), filepath.Join(p, "Microsoft/Edge/Application/msedge.exe"))
		}
	default:
		paths = append(paths, "google-chrome", "chromium", "chromium-browser")
	}
	for _, p := range paths {
		if p != "" {
			if v := findExecutable(p, ""); v != "" {
				return v
			}
		}
	}
	return ""
}
func (a *App) browserLogin(headless bool) []M {
	exe := findChrome()
	ensure(exe != "", "未找到 Chrome（Windows 也支持 Edge）。请安装浏览器或设置 AVATARTHU_CHROME 为程序路径")
	profile := filepath.Join(a.Root, "browser-profile")
	if headless {
		ensure(exists(filepath.Join(profile, "Local State")), "尚无浏览器登录记录，请运行 avatarthu login thu")
	}
	mkdir(profile)
	defer a.lock("browser-login", false)()
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))
	timeout := 10 * time.Minute
	if headless {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(a.Ctx, timeout)
	defer cancel()
	args := []string{exe, "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "--disable-save-password-bubble", "--password-store=basic"}
	page := "https://learn.tsinghua.edu.cn/f/login"
	if headless {
		args = append(args, "--headless=new")
		page = "https://learn.tsinghua.edu.cn/f/wlxt/index/course/student/"
	}
	args = append(args, page)
	cmd := command(ctx, args...)
	cmd.Dir = a.Root
	check(cmd.Start())
	cleanup := bindProcess(cmd)
	defer func() { cleanup(); _ = cmd.Wait() }()
	if !headless {
		fmt.Println("已打开 AvatarTHU 登录窗口，请完成网络学堂登录及双因素验证。")
	}
	var address string
	for address == "" {
		select {
		case <-ctx.Done():
			panic(fmt.Errorf("浏览器启动超时，请检查浏览器是否可用"))
		case <-time.After(300 * time.Millisecond):
			b, e := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
			if e == nil {
				lines := strings.Split(strings.TrimSpace(string(b)), "\n")
				if len(lines) >= 2 {
					port, e := strconv.Atoi(lines[0])
					if e == nil && port > 0 && port < 65536 {
						address = fmt.Sprintf("ws://127.0.0.1:%d%s", port, lines[1])
					}
				}
			}
		}
	}
	conn, _, e := websocket.DefaultDialer.DialContext(ctx, address, http.Header{})
	check(e)
	defer conn.Close()
	id := 0
	call := func(method string) M {
		id++
		check(conn.SetWriteDeadline(time.Now().Add(5 * time.Second)))
		check(conn.WriteJSON(M{"id": id, "method": method, "params": M{}}))
		for {
			check(conn.SetReadDeadline(time.Now().Add(5 * time.Second)))
			var v M
			check(conn.ReadJSON(&v))
			if number(v, "id", 0) == id {
				ensure(v["error"] == nil, "浏览器接口无法读取本次登录")
				return obj(v, "result")
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			panic(fmt.Errorf("网络学堂登录尚未完成，请重新运行 avatarthu login thu"))
		case <-time.After(1500 * time.Millisecond):
			v := call("Storage.getCookies")
			cookies := []M{}
			names := map[string]bool{}
			for _, c := range objects(v["cookies"]) {
				if schoolDomain(str(c, "domain")) {
					cookies = append(cookies, c)
					if strings.TrimPrefix(str(c, "domain"), ".") == "learn.tsinghua.edu.cn" {
						names[str(c, "name")] = true
					}
				}
			}
			if names["JSESSIONID"] && names["XSRF-TOKEN"] {
				if e := attempt(func() { a.saveSession(cookies) }); e == nil {
					return cookies
				}
			}
		}
	}
}
func (a *App) loginTHU(importOnly bool) {
	err := attempt(func() { a.saveSession(a.importChrome()) })
	if err != nil {
		if importOnly {
			panic(fmt.Errorf("Chrome 登录态不可用，请先在 Chrome 登录网络学堂：%s", safeError(err)))
		}
		fmt.Println("现有 Chrome 会话不可用，打开本项目内置的浏览器登录入口。")
		a.browserLogin(false)
	}
	a.keepalive(true, false)
	fmt.Println("登录成功，会话已保存：" + a.session())
}
func (a *App) keepalive(force, recoverLogin bool) M {
	defer a.lock("keepalive", true)()
	p := filepath.Join(filepath.Dir(a.session()), "keepalive-status.json")
	state := readMap(p)
	if !force && time.Since(parseTime(str(state, "checked_at"))) < 10*time.Minute {
		return state
	}
	state["checked_at"] = stamp()
	state["process_id"] = os.Getpid()
	state["interval_seconds"] = 600
	err := attempt(func() {
		s := a.school(false)
		e := attempt(func() { s.semester() })
		if e != nil {
			if _, ok := e.(sessionExpired); !ok {
				panic(e)
			}
			s.warmup()
			s.semester()
		}
		s.courses()
		s.persist()
	})
	recovered := false
	if err != nil && recoverLogin {
		_, expired := err.(sessionExpired)
		if expired && (force || time.Since(parseTime(str(state, "chrome_attempt_at"))) >= time.Hour) {
			state["chrome_attempt_at"] = stamp()
			e := attempt(func() { a.browserLogin(true) })
			recovered = e == nil
			if e == nil {
				err = nil
				state["recovery_source"] = "avatar_browser"
			} else {
				state["recovery_errors"] = M{"avatar_browser": "浏览器会话恢复未成功，需要运行 avatarthu login thu"}
			}
		}
	}
	state["recovered"] = recovered
	if err == nil {
		merge(state, M{"state": "valid", "last_success": stamp(), "message": "登录有效，已保活并保存更新后的会话"})
	} else if _, expired := err.(sessionExpired); expired {
		merge(state, M{"state": "expired", "message": "网络学堂需要重新认证，请运行 avatarthu login thu"})
	} else {
		merge(state, M{"state": "unavailable", "message": "暂时无法验证登录，保留原会话，下次重试：" + safeError(err)})
	}
	writeJSON(p, state)
	return state
}
