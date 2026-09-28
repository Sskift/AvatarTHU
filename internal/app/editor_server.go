package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"
)

func validEditorEndpoint(endpoint M) bool {
	host, port, err := net.SplitHostPort(str(endpoint, "host"))
	n, parseErr := strconv.Atoi(port)
	return err == nil && parseErr == nil && host == "127.0.0.1" && n >= 0 && n <= 65535 && regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(str(endpoint, "token"))
}

func (a *App) editorEndpoint() M {
	path := a.data("editor-endpoint.json")
	if endpoint := readMap(path); len(endpoint) > 0 {
		ensure(validEditorEndpoint(endpoint), "报告编辑页地址配置无效，请检查 "+path)
		return endpoint
	}
	endpoint := M{"host": "127.0.0.1:0", "token": randomID(32)}
	if previous, err := url.Parse(str(readMap(a.data("editor-server.json")), "url")); err == nil && previous.Scheme == "http" {
		params, err := url.ParseQuery(previous.Fragment)
		candidate := M{"host": previous.Host, "token": params.Get("token")}
		if err == nil && validEditorEndpoint(candidate) {
			endpoint = candidate
		}
	}
	writeJSON(path, endpoint)
	return endpoint
}

func (a *App) serveEditor(ctx context.Context, endpoint M) error {
	listener, err := net.Listen("tcp4", str(endpoint, "host"))
	if err != nil {
		return err
	}
	defer listener.Close()
	host := listener.Addr().String()
	if str(endpoint, "host") != host {
		endpoint["host"] = host
		writeJSON(a.data("editor-endpoint.json"), endpoint)
	}
	server := &http.Server{Handler: a.editorHandler(str(endpoint, "token"), host), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	defer server.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	writeJSON(a.data("editor-server.json"), M{"url": "http://" + host + "/#token=" + str(endpoint, "token"), "pid": os.Getpid(), "state": "running"})
	select {
	case <-ctx.Done():
		timeout, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(timeout)
		_ = server.Close()
		<-done
		return nil
	case err := <-done:
		return err
	}
}

func (a *App) startEditor(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		lastError := ""
		for ctx.Err() == nil {
			err := attempt(func() {
				endpoint := a.editorEndpoint()
				err := a.serveEditor(ctx, endpoint)
				if err != nil {
					panic(fmt.Errorf("报告编辑页无法使用固定地址 %s：%s。若端口被其他程序占用，请关闭占用程序；每 30 秒自动重试，课程后台继续运行", str(endpoint, "host"), safeError(err)))
				}
			})
			if ctx.Err() != nil {
				break
			}
			message := safeError(err)
			endpoint := readMap(a.data("editor-endpoint.json"))
			writeJSON(a.data("editor-server.json"), M{"state": "unavailable", "url": "http://" + str(endpoint, "host") + "/#token=" + str(endpoint, "token"), "error": message, "retry_at": time.Now().Add(30 * time.Second).Format(time.RFC3339), "pid": os.Getpid()})
			if message != lastError {
				fmt.Fprintln(os.Stderr, message)
				lastError = message
			}
			select {
			case <-ctx.Done():
			case <-time.After(30 * time.Second):
			}
		}
		state := readMap(a.data("editor-server.json"))
		merge(state, M{"state": "stopped", "error": "", "retry_at": ""})
		writeJSON(a.data("editor-server.json"), state)
	}()
	return func() {
		cancel()
		<-done
	}
}
