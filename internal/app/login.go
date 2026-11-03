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
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/html"
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

// Follow only the school's published SSO entry, never a credential form action.
func schoolLoginURL(page string) string {
	for _, n := range nodes(parseHTML(page), func(n *html.Node) bool { return attr(n, "id") == "loginButtonId" }) {
		match := regexp.MustCompile(`^\s*window\.location\.href\s*=\s*['"]([^'"]+)['"]\s*;?\s*$`).FindStringSubmatch(attr(n, "onclick"))
		if len(match) != 2 {
			continue
		}
		u, err := url.Parse(match[1])
		if err == nil && u.Scheme == "https" && u.Host == "id.tsinghua.edu.cn" && u.User == nil && strings.HasPrefix(u.Path, "/do/off/ui/auth/login/form/") {
			return u.String()
		}
	}
	panic(fmt.Errorf("网络学堂登录入口已改变，请运行 avatarthu login thu"))
}

func (a *App) schoolLoginURL() string {
	s := a.schoolFrom(M{})
	s.HTTP.Timeout = 15 * time.Second
	r := s.request("GET", "/f/login", nil, nil, "", false)
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	check(err)
	return schoolLoginURL(string(b))
}

func schoolLoginTarget(target M) bool {
	u, err := url.Parse(str(target, "url"))
	if err != nil || str(target, "type") != "page" || u.Host != "learn.tsinghua.edu.cn" {
		return false
	}
	return u.Path == "/f/login" || strings.Contains(u.Path, "login_timeout") || str(target, "title") == "登录超时"
}

// A clean browser exit does not retain session-only cookies. Restore missing
// school cookies from the validated native session without replacing newer ones.
func missingBrowserCookies(saved, current []M) []M {
	key := func(c M) string { return str(c, "domain") + "|" + strDefault(c, "path", "/") + "|" + str(c, "name") }
	seen := map[string]bool{}
	for _, c := range current {
		seen[key(c)] = true
	}
	missing := []M{}
	for _, c := range saved {
		if !schoolDomain(str(c, "domain")) || seen[key(c)] || str(c, "name") == "" {
			continue
		}
		cookie := M{"name": c["name"], "value": c["value"], "domain": c["domain"], "path": strDefault(c, "path", "/"), "secure": boolean(c, "secure"), "httpOnly": boolean(c, "httpOnly")}
		if expires := number(c, "expires", 0); expires > 0 {
			if int64(expires) <= time.Now().Unix() {
				continue
			}
			cookie["expires"] = expires
		}
		missing = append(missing, cookie)
		seen[key(c)] = true
	}
	return missing
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
	loginApp := *a
	loginApp.Ctx = ctx
	args := []string{exe, "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "--disable-save-password-bubble", "--password-store=basic"}
	page := "https://learn.tsinghua.edu.cn/f/login"
	if headless {
		args = append(args, "--headless=new")
		page = "about:blank"
	}
	args = append(args, page)
	// The loop observes ctx; let cleanup ask Chrome to flush its profile before
	// using the forced process-group fallback, including on timeout/cancellation.
	cmd := command(context.WithoutCancel(ctx), args...)
	cmd.Dir = a.Root
	check(cmd.Start())
	cleanup := bindProcess(cmd)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	var conn *websocket.Conn
	defer func() {
		if conn != nil {
			_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_ = conn.WriteJSON(M{"id": 0, "method": "Browser.close"})
			defer conn.Close()
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cleanup()
			<-done
		}
	}()
	if !headless {
		fmt.Println("已打开 AvatarTHU 登录窗口，请完成网络学堂登录及双因素验证。")
	}
	var address string
	for address == "" {
		select {
		case <-done:
			panic(fmt.Errorf("登录浏览器已退出，请重新运行 avatarthu login thu"))
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
	var e error
	conn, _, e = websocket.DefaultDialer.DialContext(ctx, address, http.Header{})
	check(e)
	id := 0
	call := func(method string, params M) M {
		id++
		check(conn.SetWriteDeadline(time.Now().Add(5 * time.Second)))
		check(conn.WriteJSON(M{"id": id, "method": method, "params": params}))
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
	if headless {
		current := objects(call("Storage.getCookies", M{})["cookies"])
		if missing := missingBrowserCookies(objects(readMap(a.session())["all_cookies"]), current); len(missing) > 0 {
			call("Storage.setCookies", M{"cookies": missing})
		}
		call("Target.createTarget", M{"url": schoolBase + "/f/wlxt/index/course/student/"})
	}
	ssoStarted := false
	for {
		select {
		case <-done:
			panic(fmt.Errorf("登录窗口已关闭，网络学堂登录尚未完成"))
		case <-ctx.Done():
			panic(fmt.Errorf("网络学堂登录尚未完成，请重新运行 avatarthu login thu"))
		case <-time.After(1500 * time.Millisecond):
			v := call("Storage.getCookies", M{})
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
				if e := attempt(func() { loginApp.saveSession(cookies) }); e == nil {
					return cookies
				}
			}
			if headless && !ssoStarted {
				for _, target := range objects(call("Target.getTargets", M{})["targetInfos"]) {
					if schoolLoginTarget(target) {
						call("Target.createTarget", M{"url": loginApp.schoolLoginURL()})
						ssoStarted = true
						break
					}
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
	if !force && time.Since(parseTime(str(state, "checked_at"))) < keepaliveInterval(state) {
		return state
	}
	state["checked_at"] = stamp()
	state["process_id"] = os.Getpid()
	err := attempt(func() {
		s := a.school(false)
		s.HTTP.Timeout = 25 * time.Second
		refreshSchoolSession(s)
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
		delete(state, "recovery_errors")
		delete(state, "chrome_attempt_at")
		merge(state, M{"state": "valid", "last_success": stamp(), "message": "登录有效，已保活并保存更新后的会话"})
	} else if _, expired := err.(sessionExpired); expired {
		merge(state, M{"state": "expired", "message": "网络学堂需要重新认证，请运行 avatarthu login thu"})
	} else {
		merge(state, M{"state": "unavailable", "message": "暂时无法验证登录，保留原会话，下次重试：" + safeError(err)})
	}
	state["checked_at"] = stamp()
	state["interval_seconds"] = int(keepaliveInterval(state).Seconds())
	writeJSON(p, state)
	return state
}

func keepaliveInterval(state M) time.Duration {
	if str(state, "state") == "unavailable" {
		return time.Minute
	}
	return 10 * time.Minute
}

func refreshSchoolSession(s *School) {
	// The web entry also refreshes the gateway session; API-only traffic does not
	// necessarily touch it. Persist only after authenticated course access succeeds.
	s.warmup()
	s.semester()
	s.courses()
	s.persist()
}
