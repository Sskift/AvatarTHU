// Network endpoints and cookie renewal adapted from AutoThu (MIT).
// See THIRD_PARTY.md and third_party/AutoThu-LICENSE.
package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

const schoolBase = "https://learn.tsinghua.edu.cn"

type sessionExpired struct{ message string }

func (e sessionExpired) Error() string { return e.message }
func schoolDomain(s string) bool {
	s = strings.ToLower(strings.TrimPrefix(s, "."))
	return s == "tsinghua.edu.cn" || strings.HasSuffix(s, ".tsinghua.edu.cn")
}

type schoolJar struct {
	jar     *cookiejar.Jar
	mu      sync.Mutex
	records map[string]*http.Cookie
}

func newJar() *schoolJar {
	j, e := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	check(e)
	return &schoolJar{jar: j, records: map[string]*http.Cookie{}}
}
func (j *schoolJar) Cookies(u *url.URL) []*http.Cookie { return j.jar.Cookies(u) }
func (j *schoolJar) SetCookies(u *url.URL, cs []*http.Cookie) {
	j.jar.SetCookies(u, cs)
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, c := range cs {
		v := *c
		if v.Domain == "" {
			v.Domain = u.Hostname()
		}
		if !schoolDomain(v.Domain) {
			continue
		}
		if v.Path == "" {
			v.Path = "/"
		}
		k := v.Domain + "|" + v.Path + "|" + v.Name
		if v.MaxAge < 0 || (!v.Expires.IsZero() && v.Expires.Before(time.Now())) {
			delete(j.records, k)
		} else {
			j.records[k] = &v
		}
	}
}
func (j *schoolJar) load(m M) {
	for _, c := range objects(m["all_cookies"]) {
		dom := str(c, "domain")
		if !schoolDomain(dom) {
			continue
		}
		u, e := url.Parse("https://" + strings.TrimPrefix(dom, "."))
		check(e)
		cookie := &http.Cookie{Name: str(c, "name"), Value: str(c, "value"), Domain: dom, Path: strDefault(c, "path", "/"), Secure: boolean(c, "secure"), HttpOnly: boolean(c, "httpOnly")}
		j.SetCookies(u, []*http.Cookie{cookie})
	}
	if len(j.records) == 0 {
		old := obj(m, "cookies")
		if len(old) == 0 {
			old = m
		}
		u, _ := url.Parse(schoolBase)
		for k, v := range old {
			if s, ok := v.(string); ok {
				j.SetCookies(u, []*http.Cookie{{Name: k, Value: s, Domain: u.Hostname(), Path: "/"}})
			}
		}
	}
}
func (j *schoolJar) snapshot() []M {
	j.mu.Lock()
	defer j.mu.Unlock()
	r := []M{}
	for _, c := range j.records {
		r = append(r, M{"name": c.Name, "value": c.Value, "domain": c.Domain, "path": c.Path, "secure": c.Secure, "httpOnly": c.HttpOnly})
	}
	return r
}

type School struct {
	app      *App
	HTTP     *http.Client
	Jar      *schoolJar
	Semester string
	Original []byte
	Session  string
}

func (a *App) school(warm bool) *School {
	s := a.schoolFrom(readMap(a.session()))
	s.Session = a.session()
	b, e := os.ReadFile(s.Session)
	if os.IsNotExist(e) {
		panic(sessionExpired{"尚未登录，请运行 avatarthu login thu"})
	}
	check(e)
	s.Original = b
	if warm {
		s.warmup()
		s.semester()
		s.persist()
	}
	return s
}
func (a *App) schoolFrom(m M) *School {
	jar := newJar()
	jar.load(m)
	s := &School{app: a, Jar: jar}
	s.HTTP = &http.Client{Jar: jar, Timeout: 120 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("网络学堂重定向过多")
		}
		if schoolDomain(req.URL.Hostname()) && strings.Contains(req.URL.Path, "login_timeout") {
			return sessionExpired{"网络学堂登录过期，请运行 avatarthu login thu"}
		}
		if !schoolDomain(req.URL.Hostname()) || req.URL.Scheme != "https" {
			return fmt.Errorf("网络学堂请求跳转到外部站点")
		}
		if len(via) > 0 && req.URL.Hostname() != via[0].URL.Hostname() {
			req.Header.Del("X-XSRF-TOKEN")
		}
		return nil
	}}
	return s
}
func (s *School) csrf() string {
	u, _ := url.Parse(schoolBase)
	for _, c := range s.Jar.Cookies(u) {
		if c.Name == "XSRF-TOKEN" {
			return c.Value
		}
	}
	panic(sessionExpired{"缺少登录令牌，请运行 avatarthu login thu"})
}
func cleanURL(raw string) string {
	base, _ := url.Parse(schoolBase)
	u, e := url.Parse(raw)
	check(e)
	u = base.ResolveReference(u)
	ensure(u.Scheme == "https" && u.Hostname() == "learn.tsinghua.edu.cn" && u.User == nil, "附件链接不是网络学堂 HTTPS 地址")
	// Query order is part of the legacy assignment attachment fingerprint.
	parts := []string{}
	for _, part := range strings.Split(u.RawQuery, "&") {
		if part == "" {
			continue
		}
		key, value, _ := strings.Cut(part, "=")
		key, e = url.QueryUnescape(key)
		check(e)
		value, e = url.QueryUnescape(value)
		check(e)
		if key != "_csrf" {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	u.RawQuery = strings.Join(parts, "&")
	u.Fragment = ""
	return u.String()
}
func (s *School) request(method, path string, params url.Values, body io.Reader, ctype string, csrf bool) *http.Response {
	u, e := url.Parse(cleanURL(path))
	check(e)
	q := u.Query()
	if method == "GET" {
		for k, vs := range params {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
	} else if body == nil {
		body = strings.NewReader(params.Encode())
		ctype = "application/x-www-form-urlencoded"
	}
	token := ""
	if csrf {
		token = s.csrf()
		q.Set("_csrf", token)
	}
	u.RawQuery = q.Encode()
	req, e := http.NewRequestWithContext(s.app.Ctx, method, u.String(), body)
	check(e)
	req.Header.Set("User-Agent", "Mozilla/5.0 AvatarTHU/"+Version)
	if f, ok := body.(*os.File); ok {
		info, err := f.Stat()
		check(err)
		req.ContentLength = info.Size()
	}
	if token != "" {
		req.Header.Set("X-XSRF-TOKEN", token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	r, e := s.HTTP.Do(req)
	if e != nil {
		var expired sessionExpired
		if errors.As(e, &expired) {
			panic(expired)
		}
	}
	check(e)
	if r.StatusCode == 401 || r.StatusCode == 403 {
		r.Body.Close()
		panic(sessionExpired{"网络学堂登录过期，请运行 avatarthu login thu"})
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		r.Body.Close()
		panic(fmt.Errorf("网络学堂 HTTP %d", r.StatusCode))
	}
	return r
}
func (s *School) api(method, path string, p url.Values) M {
	r := s.request(method, path, p, nil, "", true)
	defer r.Body.Close()
	var v M
	e := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&v)
	if e != nil {
		panic(sessionExpired{"网络学堂返回登录页面或无效数据，请运行 avatarthu login thu"})
	}
	return v
}
func (s *School) warmup() {
	r := s.request("GET", "/", nil, nil, "", false)
	io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	r.Body.Close()
}
func (s *School) semester() string {
	v := s.api("GET", "/b/kc/zhjw_v_code_xnxq/getCurrentAndNextSemester", nil)
	s.Semester = str(obj(v, "result"), "id")
	if s.Semester == "" {
		panic(sessionExpired{"无法验证网络学堂登录，请运行 avatarthu login thu"})
	}
	return s.Semester
}
func (s *School) courses() []M {
	if s.Semester == "" {
		s.semester()
	}
	v := s.api("POST", "/b/wlxt/kc/v_wlkc_xs_xkb_kcb_extend/student/loadCourseBySemesterId/"+url.PathEscape(s.Semester)+"/zh", nil)
	if _, ok := v["resultList"].([]any); !ok {
		panic(sessionExpired{"无法读取课程，请运行 avatarthu login thu"})
	}
	return objects(v["resultList"])
}
func (s *School) persist() {
	if s.Session == "" {
		return
	}
	defer fileLock(s.Session+".lock", true)()
	b, e := os.ReadFile(s.Session)
	check(e)
	if !bytes.Equal(b, s.Original) {
		return
	}
	v := readMap(s.Session)
	delete(v, "cookies")
	v["all_cookies"] = s.Jar.snapshot()
	writeJSON(s.Session, v)
	s.Original, e = os.ReadFile(s.Session)
	check(e)
}
func (s *School) paged(path, cid string, post bool) []M {
	all := []M{}
	seen := map[string]bool{}
	for off := 0; ; {
		p := url.Values{"aoData": {string(jsonBytes([]M{{"name": "wlkcid", "value": cid}, {"name": "sEcho", "value": 1}, {"name": "iDisplayStart", "value": off}, {"name": "iDisplayLength", "value": 100}}))}}
		method := "GET"
		if post {
			method = "POST"
		}
		v := s.api(method, path, p)
		_, ok := obj(v, "object")["aaData"].([]any)
		ensure(str(v, "result") == "success" && ok, "网络学堂返回无效列表，保留所有本地状态")
		batch := objects(obj(v, "object")["aaData"])
		hash := fingerprint(batch)
		ensure(len(batch) == 0 || !seen[hash], "网络学堂分页未前进")
		seen[hash] = true
		all = append(all, batch...)
		if len(batch) < 100 {
			return all
		}
		off += len(batch)
	}
}
func (s *School) homeworkRows(cid string) []M {
	return s.paged("/b/wlxt/kczy/zy/student/zyListWj", cid, false)
}
func eligible(hw M) bool { return parseTime(str(hw, "jzsjStr")).After(time.Now()) }
func attr(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}
func hasClass(n *html.Node, c string) bool {
	return strings.Contains(" "+attr(n, "class")+" ", " "+c+" ")
}
func nodes(n *html.Node, pred func(*html.Node) bool) []*html.Node {
	r := []*html.Node{}
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if pred(x) {
			r = append(r, x)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return r
}
func textContent(n *html.Node, sep string) string {
	r := []string{}
	for _, t := range nodes(n, func(x *html.Node) bool {
		return x.Type == html.TextNode && x.Parent != nil && x.Parent.Data != "script" && x.Parent.Data != "style"
	}) {
		if s := strings.TrimSpace(t.Data); s != "" {
			r = append(r, s)
		}
	}
	return strings.Join(r, sep)
}
func parseHTML(s string) *html.Node { n, e := html.Parse(strings.NewReader(s)); check(e); return n }
func (s *School) page(path string, pred func(*html.Node) bool) *html.Node {
	r := s.request("GET", path, nil, nil, "", true)
	defer r.Body.Close()
	n, e := html.Parse(io.LimitReader(r.Body, 16<<20))
	check(e)
	matches := nodes(n, pred)
	if len(matches) == 0 {
		panic(sessionExpired{"页面缺少预期内容，可能登录过期或网络学堂结构改变"})
	}
	return matches[0]
}
func (s *School) detail(cid string, hw M) (string, string, []any) {
	q := url.Values{"wlkcid": {cid}, "sfgq": {"0"}, "zyid": {str(hw, "zyid")}, "xszyid": {str(hw, "xszyid")}}
	box := s.page("/f/wlxt/kczy/zy/student/viewTj?"+q.Encode(), func(n *html.Node) bool { return n.Data == "div" && hasClass(n, "boxbox") })
	description := textContent(box, "\n") + "\n截止日期：" + strDefault(hw, "jzsjStr", "None")
	files := []any{}
	for _, annex := range nodes(box, func(n *html.Node) bool { return n.Data == "div" && hasClass(n, "fujian") }) {
		links := nodes(annex, func(n *html.Node) bool { return n.Data == "a" })
		label := "attachment"
		for _, n := range links {
			v := textContent(n, "")
			if v != "" && v != "下载" && v != "Download" && v != "预览" && v != "Preview" {
				label = safeName(v)
				break
			}
		}
		for _, n := range links {
			h := attr(n, "href")
			u, e := url.Parse(h)
			if e != nil {
				continue
			}
			if strings.HasPrefix(u.Path, "/b/") && (strings.Contains(strings.ToLower(h), "download") || strings.Contains(strings.ToLower(h), "xz")) {
				files = append(files, []any{label, cleanURL(h)})
			}
		}
	}
	for _, calendar := range nodes(box, func(n *html.Node) bool { return n.Data == "div" && hasClass(n, "calendar") }) {
		for i, n := range nodes(calendar, func(n *html.Node) bool { return n.Data == "img" && attr(n, "src") != "" }) {
			u := cleanURL(attr(n, "src"))
			parsed, _ := url.Parse(u)
			ext := filepath.Ext(parsed.Path)
			if ext == "" {
				ext = ".png"
			}
			files = append(files, []any{fmt.Sprintf("figure-%d%s", i+1, ext), u})
		}
	}
	ensure(str(hw, "zyfjid") == "" || len(files) > 0, "发现附件标识但未解析到下载链接")
	return str(hw, "bt"), description, files
}
func (s *School) download(raw, target, version string) {
	receipt := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".download.json")
	old := readMap(receipt)
	if exists(target) && str(old, "version") == version && str(old, "sha256") == digest(target) {
		return
	}
	r := s.request("GET", raw, nil, nil, "", true)
	defer r.Body.Close()
	ext := strings.ToLower(filepath.Ext(target))
	ensure(!strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "text/html") || ext == ".html" || ext == ".htm", "附件接口返回登录页面，未覆盖本地文件")
	ensure(r.Request.URL.Hostname() == "learn.tsinghua.edu.cn", "附件请求跳转到登录站点")
	mkdir(filepath.Dir(target))
	f, e := os.CreateTemp(filepath.Dir(target), ".download-")
	check(e)
	defer os.Remove(f.Name())
	defer f.Close()
	n, e := io.Copy(f, r.Body)
	check(e)
	ensure(n > 0 && (r.ContentLength < 0 || r.ContentLength == n), "附件下载不完整，下次重试")
	check(f.Sync())
	check(f.Close())
	check(replaceFile(f.Name(), target))
	writeJSON(receipt, M{"version": version, "sha256": digest(target)})
}
func (s *School) courseDir(course M) string {
	root := filepath.Join(s.app.Root, "courses")
	legacy := s.app.data("courses")
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		root = legacy
	}
	dir := filepath.Join(root, safeName(s.Semester), safeName(str(course, "kcm"))+"--"+fingerprint(course["wlkcid"])[:8])
	mkdir(dir)
	writeJSON(filepath.Join(dir, "course.json"), course)
	writeFile(filepath.Join(dir, "README.md"), []byte("# "+str(course, "kcm")+"\n\n学期："+s.Semester+"\n\n- notices/：公告\n- courseware/：课件\n- homework/：题目、历次执行、产物与复审\n"), 0600)
	return dir
}
func (s *School) courseware(cid, dir string) {
	v := s.api("GET", "/b/wlxt/kj/wlkc_kjxxb/student/kjxxbByWlkcidAndSizeForStudent", url.Values{"wlkcid": {cid}, "size": {"10000"}})
	_, ok := v["object"].([]any)
	ensure(str(v, "result") == "success" && ok, "无效课件列表")
	files := objects(v["object"])
	ensure(len(files) < 10000, "课件列表超过分页上限")
	index := []M{}
	mkdir(dir)
	for _, f := range files {
		name := safeName(str(f, "bt"))
		ext := strings.TrimLeft(str(f, "wjlx"), ".")
		if ext != "" && !strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(ext)) {
			name += "." + safeName(ext)
		}
		rel := fingerprint(f["wjid"])[:12] + "/" + name
		path := filepath.Join(dir, filepath.FromSlash(rel))
		s.download("/b/wlxt/kj/wlkc_kjxxb/student/downloadFile?"+url.Values{"sfgk": {"0"}, "wjid": {str(f, "wjid")}}.Encode(), path, fingerprint([]any{f["wjid"], f["scsj"], f["wjdx"]}))
		index = append(index, M{"path": rel, "title": f["bt"], "description": f["ms"], "sha256": digest(path)})
	}
	sort.Slice(index, func(i, j int) bool { return str(index[i], "path") < str(index[j], "path") })
	writeJSON(filepath.Join(dir, "index.json"), index)
}
func (s *School) assignments() []M {
	found := []M{}
	for _, course := range s.courses() {
		cid := str(course, "wlkcid")
		dir := s.courseDir(course)
		s.courseware(cid, filepath.Join(dir, "courseware"))
		for _, hw := range s.homeworkRows(cid) {
			if str(hw, "jzsjStr") != "" && !eligible(hw) {
				continue
			}
			title, desc, attachments := s.detail(cid, hw)
			assignment := filepath.Join(dir, "homework", safeName(title)+"--"+fingerprint(hw["xszyid"])[:8])
			source := filepath.Join(assignment, "source")
			writeFile(filepath.Join(source, "README.md"), []byte(desc), 0600)
			seen := map[string]bool{}
			for _, f := range attachments {
				pair := texts(f)
				ensure(!seen[pair[0]], "附件重名，需要核对："+pair[0])
				seen[pair[0]] = true
				s.download(pair[1], filepath.Join(source, pair[0]), fingerprint([]any{pair[1], hw["scsj"], desc}))
			}
			meta := M{"course_id": cid, "course": course["kcm"], "semester": s.Semester, "xszyid": hw["xszyid"], "zyid": hw["zyid"], "title": title, "deadline": strDefault(hw, "jzsjStr", "未提供"), "description": desc, "folder": source, "assignment_dir": assignment, "courseware": filepath.Join(dir, "courseware"), "requirements_hash": fingerprint([]any{desc, attachments, hw["zyfjid"], hw["scsj"]})}
			writeJSON(filepath.Join(assignment, "assignment.json"), meta)
			found = append(found, meta)
		}
	}
	s.persist()
	return found
}
func (s *School) announcementRows(cid, kind string) []M {
	return s.paged("/b/wlxt/kcgg/wlkc_ggb/student/pageListXsby"+kind, cid, true)
}
func (s *School) announcements() []M {
	all := []M{}
	for _, course := range s.courses() {
		dir := filepath.Join(s.courseDir(course), "notices")
		for _, kind := range []string{"Wgq", "Ygq"} {
			for _, r := range s.announcementRows(str(course, "wlkcid"), kind) {
				body := str(r, "ggnrStr")
				if str(r, "ggnr") != "" {
					b, e := base64.StdEncoding.DecodeString(str(r, "ggnr"))
					check(e)
					body = string(b)
				}
				sfyd := r["sfyd"]
				unread := sfyd == "否" || sfyd == "0" || sfyd == false || sfyd == float64(0)
				n := M{"id": str(r, "ggid"), "course_id": course["wlkcid"], "course": course["kcm"], "title": str(r, "bt"), "date": str(r, "fbsjStr"), "kind": kind, "body": textContent(parseHTML(body), "\n"), "unread": unread, "attachment_name": r["fjmc"]}
				writeJSON(filepath.Join(dir, safeName(str(n, "id"))+".json"), n)
				writeFile(filepath.Join(dir, safeName(str(n, "id"))+".md"), []byte("# "+str(n, "title")+"\n\n"+str(n, "date")+"\n\n"+str(n, "body")), 0600)
				all = append(all, n)
			}
		}
	}
	return all
}
func (s *School) markRead(n M) {
	s.page("/f/wlxt/kcgg/wlkc_ggb/student/beforeViewXs?"+url.Values{"wlkcid": {str(n, "course_id")}, "id": {str(n, "id")}}.Encode(), func(x *html.Node) bool { return attr(x, "id") == "editFormId" })
	ok := false
	for _, r := range s.announcementRows(str(n, "course_id"), str(n, "kind")) {
		if str(r, "ggid") == str(n, "id") && str(r, "sfyd") == "是" {
			ok = true
		}
	}
	ensure(ok, "公告已送达但网络学堂未确认已读，下次只重试标记")
	s.persist()
}
func (a *App) verifyOpen(st M) *School {
	if a.Verify != nil {
		return a.Verify(st)
	}
	s := a.school(true)
	var hw M
	for _, h := range s.homeworkRows(str(st, "course_id")) {
		if h["xszyid"] == st["xszyid"] {
			hw = h
			break
		}
	}
	ensure(hw != nil && eligible(hw), "作业已交、截止或已不在待交列表，未执行上传")
	title, desc, attachments := s.detail(str(st, "course_id"), hw)
	ensure(title == str(st, "title") && desc == str(st, "description") && str(hw, "jzsjStr") == str(st, "deadline"), "作业要求或截止时间已改变，请先同步并重新处理")
	hash := fingerprint([]any{desc, attachments, hw["zyfjid"], hw["scsj"]})
	ensure(str(st, "requirements_hash") == "" || str(st, "requirements_hash") == hash, "作业附件已更新，请先同步并重新处理")
	return s
}
