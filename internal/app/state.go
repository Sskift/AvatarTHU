package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

type M = map[string]any

var Version = "0.2.0-alpha.1"
var beijing = time.FixedZone("Asia/Shanghai", 8*3600)

type App struct {
	Root     string
	Ctx      context.Context
	RunModel func(string, string, string, M, string, M) M
	SendCard func(M, string) M
	Verify   func(M) *School
	Upload   func(*School, M, string) M
	CallLark func([]string) M
}

func New(ctx context.Context, root string) *App {
	if root == "" {
		root = defaultRoot()
	}
	root = abs(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	a := &App{Root: root, Ctx: ctx}
	return a
}
func defaultRoot() string {
	if s := os.Getenv("AVATARTHU_HOME"); s != "" {
		return s
	}
	home, e := os.UserHomeDir()
	check(e)
	if runtime.GOOS == "windows" {
		return filepath.Join(home, ".avatarthu")
	}
	alias := filepath.Join(home, ".avatarthu")
	if st, e := os.Lstat(alias); e == nil && st.Mode()&os.ModeSymlink == 0 && st.IsDir() {
		return alias
	}
	return filepath.Join(home, ".local", "share", "avatarthu")
}
func (a *App) data(parts ...string) string {
	return filepath.Join(append([]string{a.Root, "data"}, parts...)...)
}
func (a *App) config() M      { return readMap(filepath.Join(a.Root, "config.json")) }
func (a *App) saveConfig(m M) { writeJSON(filepath.Join(a.Root, "config.json"), m) }
func (a *App) session() string {
	return strDefault(a.config(), "session", filepath.Join(a.Root, "session.json"))
}
func (a *App) enabled() bool {
	m := a.config()
	if _, ok := m["lark_enabled"]; ok {
		return boolean(m, "lark_enabled")
	}
	return str(m, "lark_user_id") != ""
}
func (a *App) taskPath(tid string) string {
	ensure(regexp.MustCompile(`^[a-f0-9]{16}$`).MatchString(tid), "无效作业编号")
	return a.data("tasks", tid+".json")
}
func (a *App) saveTask(st M) {
	st["updated_at"] = stamp()
	writeJSON(a.taskPath(str(st, "task_id")), st)
	if dir := str(st, "assignment_dir"); dir != "" {
		writeJSON(filepath.Join(dir, "state.json"), st)
	}
}
func (a *App) tasks() []M {
	paths, e := filepath.Glob(a.data("tasks", "*.json"))
	check(e)
	sort.Strings(paths)
	result := []M{}
	for _, p := range paths {
		result = append(result, readMap(p))
	}
	return result
}
func (a *App) lock(name string, blocking bool) func() {
	return fileLock(a.data("locks", name+".lock"), blocking)
}
func fileLock(p string, block bool) func() {
	mkdir(filepath.Dir(p))
	l := flock.New(p)
	if block {
		check(l.Lock())
	} else {
		ok, e := l.TryLock()
		check(e)
		ensure(ok, "已有进程正在处理："+filepath.Base(p))
	}
	return func() { check(l.Unlock()) }
}
func readMap(p string) M {
	b, e := os.ReadFile(p)
	if os.IsNotExist(e) {
		return M{}
	}
	check(e)
	var m M
	check(json.Unmarshal(b, &m))
	ensure(m != nil, "无效 JSON 状态："+p)
	return m
}
func jsonBytes(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	check(e.Encode(v))
	return bytes.TrimSpace(b.Bytes())
}
func writeJSON(p string, v any) {
	// Keep unchanged legacy bytes: courseware index files participate in the
	// material hash. A formatting-only rewrite must not invalidate old cards.
	if old, err := os.ReadFile(p); err == nil {
		var decoded any
		if json.Unmarshal(old, &decoded) == nil && bytes.Equal(jsonBytes(decoded), jsonBytes(v)) {
			return
		}
	}
	var b bytes.Buffer
	check(json.Indent(&b, jsonBytes(v), "", "  "))
	b.WriteByte('\n')
	writeFile(p, b.Bytes(), 0600)
}
func writeFile(p string, b []byte, mode os.FileMode) {
	mkdir(filepath.Dir(p))
	f, e := os.CreateTemp(filepath.Dir(p), ".avatar-")
	check(e)
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	check(f.Chmod(mode))
	_, e = f.Write(b)
	check(e)
	check(f.Sync())
	check(f.Close())
	check(replaceFile(tmp, p))
}
func copyFile(src, dst string) {
	f, e := os.Open(src)
	check(e)
	defer f.Close()
	mkdir(filepath.Dir(dst))
	out, e := os.CreateTemp(filepath.Dir(dst), ".copy-")
	check(e)
	defer os.Remove(out.Name())
	defer out.Close()
	check(out.Chmod(0600))
	_, e = io.Copy(out, f)
	check(e)
	check(out.Sync())
	check(out.Close())
	check(replaceFile(out.Name(), dst))
}
func digest(p string) string {
	f, e := os.Open(p)
	check(e)
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	check(e)
	return hex.EncodeToString(h.Sum(nil))
}

// Legacy IDs use Python's sort_keys=True, ensure_ascii=False, default separators.
// Preserve them exactly so already delivered cards remain bound to their task.
func canonical(v any) string {
	switch x := v.(type) {
	case M:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := []string{}
		for _, k := range keys {
			parts = append(parts, quoteLegacy(k)+": "+canonical(x[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := []string{}
		for _, item := range x {
			parts = append(parts, canonical(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		items := []any{}
		for _, s := range x {
			items = append(items, s)
		}
		return canonical(items)
	case []M:
		items := []any{}
		for _, s := range x {
			items = append(items, s)
		}
		return canonical(items)
	case string:
		return quoteLegacy(x)
	default:
		return string(jsonBytes(v))
	}
}
func quoteLegacy(s string) string {
	b := string(jsonBytes(s))
	return strings.ReplaceAll(strings.ReplaceAll(b, `\u2028`, "\u2028"), `\u2029`, "\u2029")
}
func fingerprint(v any) string {
	h := sha256.Sum256([]byte(canonical(v)))
	return hex.EncodeToString(h[:])
}
func str(m M, k string) string {
	v := m[k]
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return fmt.Sprint(v)
}
func strDefault(m M, k, d string) string {
	if s := str(m, k); s != "" {
		return s
	}
	return d
}
func number(m M, k string, d int) int {
	switch n := m[k].(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return d
}
func boolean(m M, k string) bool { v, _ := m[k].(bool); return v }
func obj(m M, k string) M {
	if v, ok := m[k].(M); ok && v != nil {
		return v
	}
	return M{}
}
func objects(v any) []M {
	r := []M{}
	switch a := v.(type) {
	case []M:
		return a
	case []any:
		for _, x := range a {
			m, ok := x.(M)
			ensure(ok, "预期 JSON 对象列表")
			r = append(r, m)
		}
	}
	return r
}
func texts(v any) []string {
	r := []string{}
	switch a := v.(type) {
	case []string:
		return a
	case []any:
		for _, x := range a {
			s, ok := x.(string)
			ensure(ok, "预期文本列表")
			r = append(r, s)
		}
	}
	return r
}
func merge(dst, src M) {
	for k, v := range src {
		dst[k] = v
	}
}
func check(e error) {
	if e != nil {
		panic(e)
	}
}
func ensure(ok bool, msg string) {
	if !ok {
		panic(fmt.Errorf("%s", msg))
	}
}
func attempt(f func()) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if e, ok := p.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("%v", p)
			}
		}
	}()
	f()
	return
}
func stamp() string { return time.Now().In(beijing).Format(time.RFC3339Nano) }
func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, e := time.ParseInLocation(layout, s, beijing); e == nil {
			return t
		}
	}
	return time.Time{}
}
func mkdir(p string) { check(os.MkdirAll(p, 0700)) }
func abs(p string) string {
	if strings.HasPrefix(p, "~/") {
		h, e := os.UserHomeDir()
		check(e)
		p = filepath.Join(h, p[2:])
	}
	v, e := filepath.Abs(p)
	check(e)
	return v
}
func exists(p string) bool { s, e := os.Stat(p); return e == nil && !s.IsDir() }
func randomID(n int) string {
	b := make([]byte, n)
	_, e := rand.Read(b)
	check(e)
	return hex.EncodeToString(b)
}
func safeError(v any) string {
	return regexp.MustCompile(`(?i)((?:_csrf|access_token|refresh_token|app_secret)=)[^&\s'"<>]+`).ReplaceAllString(fmt.Sprint(v), "${1}[redacted]")
}
func safeName(s string) string {
	s = regexp.MustCompile(`[\\/\x00-\x1f:*?"<>|]`).ReplaceAllString(s, "_")
	s = strings.Trim(s, " .")
	r := []rune(s)
	if len(r) > 100 {
		s = string(r[:100])
	}
	if s == "" {
		return "untitled"
	}
	switch strings.ToUpper(strings.Split(s, ".")[0]) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		s = "_" + s
	}
	return s
}
func relative(base, p string) string {
	r, e := filepath.Rel(abs(base), abs(p))
	check(e)
	ensure(r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)), "文件不在工作目录内")
	return r
}
func inside(base, rel string) string {
	ensure(!filepath.IsAbs(rel) && !strings.Contains(rel, ":"), "产物必须为可见相对路径")
	parts := strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' })
	ensure(len(parts) > 0, "空产物路径")
	p := abs(base)
	for _, s := range parts {
		ensure(!strings.HasPrefix(s, "."), "不允许隐藏或越界产物")
		p = filepath.Join(p, s)
		st, e := os.Lstat(p)
		check(e)
		ensure(st.Mode()&os.ModeSymlink == 0, "不允许符号链接产物")
	}
	st, e := os.Stat(p)
	check(e)
	ensure(st.Mode().IsRegular() && st.Size() > 0, "产物缺失或为空："+rel)
	return p
}
func visibleFiles(base string) []string {
	out := []string{}
	check(filepath.WalkDir(base, func(p string, d os.DirEntry, e error) error {
		if os.IsNotExist(e) && p == base {
			return nil
		}
		if e != nil {
			return e
		}
		if p != base && (strings.HasPrefix(d.Name(), ".") || d.Type()&os.ModeSymlink != 0) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			out = append(out, p)
		}
		return nil
	}))
	sort.Strings(out)
	return out
}
func copyTree(src, dst string) {
	mkdir(dst)
	for _, p := range visibleFiles(src) {
		copyFile(p, filepath.Join(dst, relative(src, p)))
	}
}
func tail(p string, n int) string {
	f, e := os.Open(p)
	if e != nil {
		return ""
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return ""
	}
	start := st.Size() - int64(n)
	if start < 0 {
		start = 0
	}
	_, e = f.Seek(start, 0)
	if e != nil {
		return ""
	}
	b, e := io.ReadAll(io.LimitReader(f, int64(n)))
	if e != nil {
		return ""
	}
	return string(b)
}
