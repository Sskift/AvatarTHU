package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

func remoteKeepaliveArgs(remote M) []string {
	host, binary := str(remote, "host"), str(remote, "binary")
	ensure(regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.@-]*$`).MatchString(host), "远端保活需要有效的 SSH 主机名")
	// SSH sends a remote shell command. Restrict the executable path rather
	// than interpolating shell syntax or accepting an arbitrary command.
	ensure(regexp.MustCompile(`^[a-zA-Z0-9_./:][a-zA-Z0-9_./:\\-]*$`).MatchString(binary), "远端程序路径不能包含空格或 shell 字符")
	return []string{"ssh", "-T", "-o", "RequestTTY=no", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ClearAllForwardings=yes", "-o", "ConnectTimeout=10", host, binary, "keepalive", "exchange"}
}

func (a *App) remoteLoginPendingPath() string {
	return filepath.Join(filepath.Dir(a.session()), "remote-login-pending.json")
}

func (a *App) remoteKeepalive(force bool) M {
	remote := obj(a.config(), "keepalive_remote")
	argv := remoteKeepaliveArgs(remote)
	var original []byte
	pendingID := ""
	func() {
		defer fileLock(a.session()+".lock", true)()
		var err error
		original, err = os.ReadFile(a.session())
		ensure(err == nil || os.IsNotExist(err), "无法读取本机会话")
		pendingID = str(readMap(a.remoteLoginPendingPath()), "id")
	}()
	replace := pendingID != ""
	session := M{}
	if len(original) > 0 {
		session = parseMap(original)
	}
	ctx, cancel := context.WithTimeout(a.Ctx, 2*time.Minute)
	defer cancel()
	cmd := command(ctx, argv...)
	cmd.Dir = a.Root
	cmd.Stdin = bytes.NewReader(jsonBytes(M{"session": session, "replace": replace, "force": force}))
	var output bytes.Buffer
	cmd.Stdout = &output
	// Never relay protocol output or stderr: both can contain session data.
	check(cmd.Start())
	defer bindProcess(cmd)()
	ensure(cmd.Wait() == nil, "无法通过 SSH 更新远端保活会话")
	ensure(output.Len() <= 2<<20, "远端保活响应过大")
	result := parseMap(output.Bytes())
	return a.receiveRemoteKeepalive(original, pendingID, result)
}

func (a *App) receiveRemoteKeepalive(original []byte, pendingID string, result M) M {
	if pendingID != "" {
		ensure(str(obj(result, "status"), "state") == "valid" && boolean(result, "accepted"), "远端未接受新登录，请稍后重试")
	}
	state := a.applyRemoteKeepalive(original, result)
	state["source"] = "ssh:" + str(obj(a.config(), "keepalive_remote"), "host")
	if pendingID != "" {
		func() {
			defer fileLock(a.session()+".lock", true)()
			if pendingID != "" && str(readMap(a.remoteLoginPendingPath()), "id") == pendingID {
				check(os.Remove(a.remoteLoginPendingPath()))
			}
		}()
	}
	return state
}

func (a *App) applyRemoteKeepalive(original []byte, result M) M {
	state := obj(result, "status")
	ensure(str(state, "state") == "valid" || str(state, "state") == "expired" || str(state, "state") == "unavailable", "远端保活响应无效")
	if str(state, "state") != "valid" {
		return state
	}
	checked := parseTime(str(state, "checked_at"))
	ensure(!checked.IsZero() && time.Since(checked) < 15*time.Minute && time.Until(checked) < time.Minute, "远端保活状态已过期")
	session := obj(result, "session")
	s := a.schoolFrom(session)
	s.csrf()
	defer fileLock(a.session()+".lock", true)()
	current, err := os.ReadFile(a.session())
	ensure(err == nil || os.IsNotExist(err), "无法读取本机会话")
	// A login completed while SSH was in flight must win over this response.
	if bytes.Equal(current, original) {
		writeJSON(a.session(), session)
	}
	return state
}

func (a *App) exchangeKeepalive(request M) M {
	ensure(str(obj(a.config(), "keepalive_remote"), "host") == "", "远端保活不能继续转发到另一台主机")
	defer a.lock("keepalive", true)()
	candidate := obj(request, "session")
	accepted := false
	accept := func() {
		a.acceptKeepaliveSession(candidate, func(m M) M {
			s := a.schoolFrom(m)
			s.HTTP.Timeout = 25 * time.Second
			refreshSchoolSession(s)
			return M{"all_cookies": s.Jar.snapshot()}
		})
		accepted = true
	}
	var state M
	if boolean(request, "replace") {
		check(attempt(accept))
		state = readMap(filepath.Join(filepath.Dir(a.session()), "keepalive-status.json"))
	} else {
		state = a.localKeepalive(boolean(request, "force"), true)
		if str(state, "state") == "expired" && len(candidate) > 0 {
			if err := attempt(accept); err == nil {
				state = readMap(filepath.Join(filepath.Dir(a.session()), "keepalive-status.json"))
			}
		}
	}
	result := M{"status": state, "accepted": accepted}
	if str(state, "state") == "valid" {
		defer fileLock(a.session()+".lock", true)()
		result["session"] = readMap(a.session())
	}
	return result
}

func (a *App) acceptKeepaliveSession(candidate M, validate func(M) M) {
	ensure(len(candidate) > 0, "没有可同步的登录会话")
	original, err := os.ReadFile(a.session())
	ensure(err == nil || os.IsNotExist(err), "无法读取远端会话")
	validated := validate(candidate)
	defer fileLock(a.session()+".lock", true)()
	current, err := os.ReadFile(a.session())
	ensure(err == nil || os.IsNotExist(err), "无法读取远端会话")
	ensure(bytes.Equal(current, original), "登录状态已更新，请重试同步")
	writeJSON(a.session(), validated)
	writeJSON(filepath.Join(filepath.Dir(a.session()), "keepalive-status.json"), M{
		"state": "valid", "checked_at": stamp(), "last_success": stamp(), "interval_seconds": 600,
		"process_id": os.Getpid(), "recovered": false, "message": "登录有效，已保活并保存更新后的会话",
	})
}
