package app

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Only the tool category and a filename from an assignment directory cross
// into the workspace. Prompts, model text, shell commands and tool results stay
// outside this projection. The sidecar lives alongside the isolated job.
var activityLabels = map[string]string{
	"read": "读取文件", "search": "检索资料", "write": "编辑文件",
	"command": "执行命令", "web": "查询网络资料", "list": "列出文件",
	"test": "运行测试", "build": "构建产物", "check": "运行静态检查",
}

type activityEvent struct {
	Action string `json:"action"`
	File   string `json:"file,omitempty"`
	At     string `json:"at"`
	Count  int    `json:"count,omitempty"`
}

type activityLog struct {
	Events []activityEvent `json:"events"`
}

func activityFilename(job, name string) string {
	if name == "" {
		return ""
	}
	if filepath.IsAbs(name) {
		var err error
		name, err = filepath.Rel(job, filepath.Clean(name))
		if err != nil {
			return ""
		}
	}
	name = filepath.ToSlash(filepath.Clean(name))
	first, rest, ok := strings.Cut(name, "/")
	switch first {
	case "input", "final", "previous-final", "candidate", "submission", "owner-final":
	default:
		return ""
	}
	if !ok || !filepath.IsLocal(filepath.FromSlash(rest)) {
		return ""
	}
	for _, part := range strings.Split(rest, "/") {
		if strings.HasPrefix(part, ".") || strings.ContainsAny(part, "\\:\r\n") {
			return ""
		}
	}
	return filepath.Base(filepath.FromSlash(rest))
}

// Match only simple commands. CLI wrappers commonly report a shell -lc
// invocation. Leave pipelines, substitutions and compound scripts unclassified.
func commandActivity(command string) string {
	command = strings.TrimSpace(command)
	if len(command) > 16000 || strings.ContainsAny(command, ";&|<>$`\\(){}\r\n") {
		return "command"
	}
	first, remainder, _ := strings.Cut(command, " ")
	switch filepath.Base(first) {
	case "sh", "bash", "zsh":
		flag, body, _ := strings.Cut(strings.TrimSpace(remainder), " ")
		body = strings.TrimSpace(body)
		if (flag != "-lc" && flag != "-c") || len(body) < 2 || (body[0] != '\'' && body[0] != '"') || body[len(body)-1] != body[0] {
			return "command"
		}
		quote := body[0]
		body = body[1 : len(body)-1]
		if strings.IndexByte(body, quote) >= 0 {
			return "command"
		}
		command = body
	}
	args := strings.Fields(command)
	if len(args) == 0 {
		return "command"
	}
	switch args[0] {
	case "rg", "grep":
		return "search"
	case "ls":
		return "list"
	case "cat", "head", "tail":
		return "read"
	case "pytest":
		return "test"
	}
	if len(args) > 1 {
		switch args[0] + " " + args[1] {
		case "go test", "cargo test", "npm test":
			return "test"
		case "go build", "cargo build":
			return "build"
		case "go vet", "cargo check":
			return "check"
		case "npm run":
			if len(args) > 2 && args[2] == "build" {
				return "build"
			}
			if len(args) > 2 && args[2] == "test" {
				return "test"
			}
		}
	}
	return "command"
}

type cliEvent struct {
	Type string `json:"type"`
	Item struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	} `json:"item"`
	Message json.RawMessage `json:"message"`
}

func (log *activityLog) add(job, action, path string) {
	event := activityEvent{Action: action, File: activityFilename(job, path), At: stamp(), Count: 1}
	if n := len(log.Events); n > 0 && log.Events[n-1].Action == event.Action && log.Events[n-1].File == event.File {
		event.Count += log.Events[n-1].Count
		log.Events[n-1] = event
	} else {
		log.Events = append(log.Events, event)
		if len(log.Events) > 12 {
			log.Events = log.Events[1:]
		}
	}
}

func (log *activityLog) observe(job string, e cliEvent) bool {
	switch e.Type {
	case "assistant":
		var message struct {
			Content []struct {
				Type  string `json:"type"`
				Name  string `json:"name"`
				Input struct {
					File    string `json:"file_path"`
					Command string `json:"command"`
				} `json:"input"`
			} `json:"content"`
		}
		if json.Unmarshal(e.Message, &message) != nil {
			return false
		}
		changed := false
		for _, use := range message.Content {
			if use.Type != "tool_use" {
				continue
			}
			var action string
			switch use.Name {
			case "Read":
				action = "read"
			case "Write", "Edit":
				action = "write"
			case "Grep", "Glob":
				action = "search"
			case "Bash":
				action = commandActivity(use.Input.Command)
			case "WebFetch", "WebSearch":
				action = "web"
			default:
				continue
			}
			log.add(job, action, use.Input.File)
			changed = true
		}
		return changed
	case "item.started":
		switch e.Item.Type {
		case "command_execution":
			log.add(job, commandActivity(e.Item.Command), "")
		case "web_search":
			log.add(job, "web", "")
		default:
			return false
		}
		return true
	case "item.completed":
		if e.Item.Type == "file_change" && len(e.Item.Changes) > 0 {
			for _, change := range e.Item.Changes {
				log.add(job, "write", change.Path)
			}
			return true
		}
	}
	return false
}

// Tee Codex events into its existing private JSONL. Claude streams tool
// events, but only its final result envelope belongs in claude.json. Cmd.Wait
// drains stdout before finish closes the pipe and joins the decoder.
func observeCLI(executor, job, role string, output *os.File) (stdout io.Writer, finish func() error) {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := attempt(func() {
			var activity activityLog
			decoder := json.NewDecoder(reader)
			for {
				var raw json.RawMessage
				err := decoder.Decode(&raw)
				if err == io.EOF {
					return
				}
				check(err)
				var e cliEvent
				// A diagnostic message may use a different shape. Terminal
				// validation still reads the executor's original result below.
				if json.Unmarshal(raw, &e) != nil {
					continue
				}
				if executor == "claude" && e.Type == "result" {
					_, err = output.Write(raw)
					check(err)
				}
				if (role == "writer" || role == "reviewer") && activity.observe(job, e) {
					// Display telemetry is secondary to producing the deliverable.
					_ = attempt(func() { writeJSON(job+"-activity.json", activity) })
				}
			}
		})
		// Keep draining even if progress parsing fails, so the child's exit
		// and diagnostics remain observable and no process blocks on stdout.
		_, _ = io.Copy(io.Discard, reader)
		reader.Close()
		done <- err
	}()
	stdout = writer
	if executor == "codex" {
		stdout = io.MultiWriter(output, writer)
	}
	return stdout, sync.OnceValue(func() error { writer.Close(); return <-done })
}
