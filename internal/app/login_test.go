package app

import (
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestKeepaliveRefreshesWebSessionBeforeAuthenticatedAPIs(t *testing.T) {
	a := testApp(t)
	paths := []string{}
	s := fakeSchool(a, func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/":
			result := response(r, "<html>courses</html>", "text/html", -1)
			result.Header.Add("Set-Cookie", "gateway=renewed; Path=/; Max-Age=3600")
			return result, nil
		case "/b/kc/zhjw_v_code_xnxq/getCurrentAndNextSemester":
			if c, err := r.Cookie("gateway"); err != nil || c.Value != "renewed" {
				t.Fatal("authenticated request did not use the refreshed gateway cookie")
			}
			return response(r, `{"result":{"id":"semester"}}`, "application/json", -1), nil
		default:
			return response(r, `{"resultList":[]}`, "application/json", -1), nil
		}
	})
	writeJSON(a.session(), M{"all_cookies": s.Jar.snapshot()})
	s.Session = a.session()
	s.Original, _ = os.ReadFile(s.Session)
	refreshSchoolSession(s)
	want := []string{"/", "/b/kc/zhjw_v_code_xnxq/getCurrentAndNextSemester", "/b/wlxt/kc/v_wlkc_xs_xkb_kcb_extend/student/loadCourseBySemesterId/semester/zh"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("refresh requests: %v", paths)
	}
	found := false
	for _, c := range objects(readMap(a.session())["all_cookies"]) {
		if str(c, "name") == "gateway" {
			found = str(c, "value") == "renewed" && number(c, "expires", 0) > int(time.Now().Unix())
		}
	}
	if !found {
		t.Fatal("refreshed gateway cookie and its expiry were not saved")
	}
}

func TestKeepaliveDoesNotSaveAnonymousReplacement(t *testing.T) {
	a := testApp(t)
	s := fakeSchool(a, func(r *http.Request) (*http.Response, error) {
		result := response(r, `{"result":{}}`, "application/json", -1)
		result.Header.Add("Set-Cookie", "JSESSIONID=anonymous; Path=/")
		return result, nil
	})
	writeJSON(a.session(), M{"all_cookies": s.Jar.snapshot()})
	s.Session = a.session()
	s.Original, _ = os.ReadFile(s.Session)
	expectError(t, "登录", func() { refreshSchoolSession(s) })
	after, _ := os.ReadFile(s.Session)
	if string(after) != string(s.Original) {
		t.Fatal("failed probe replaced saved login")
	}
}

func TestKeepaliveRetriesTransientFailureSooner(t *testing.T) {
	for _, test := range []struct {
		state string
		want  time.Duration
	}{{"valid", 10 * time.Minute}, {"unavailable", time.Minute}, {"expired", 10 * time.Minute}} {
		if got := keepaliveInterval(M{"state": test.state}); got != test.want {
			t.Fatalf("%s: %v", test.state, got)
		}
	}
}

func TestBrowserRecoveryUsesPublishedSSOEntry(t *testing.T) {
	want := "https://id.tsinghua.edu.cn/do/off/ui/auth/login/form/application/0"
	page := `<input id="loginButtonId" onclick="window.location.href='` + want + `'" type="button" value="登录">`
	if got := schoolLoginURL(page); got != want {
		t.Fatal(got)
	}
	for _, bad := range []string{
		"https://id.tsinghua.edu.cn.attacker.invalid/do/off/ui/auth/login/form/application/0",
		"http://id.tsinghua.edu.cn/do/off/ui/auth/login/form/application/0",
		"https://id.tsinghua.edu.cn/do/off/ui/auth/login/check/application/0",
		"https://user@id.tsinghua.edu.cn/do/off/ui/auth/login/form/application/0",
	} {
		expectError(t, "入口", func() { schoolLoginURL(strings.ReplaceAll(page, want, bad)) })
	}
	// The school also serves its timeout page at the original course URL.
	if !schoolLoginTarget(M{"type": "page", "url": schoolBase + "/f/wlxt/index/course/student/", "title": "登录超时"}) {
		t.Fatal("inline timeout page did not trigger SSO recovery")
	}
	if schoolLoginTarget(M{"type": "page", "url": "https://unrelated.invalid/", "title": "登录超时"}) {
		t.Fatal("unrelated page triggered recovery")
	}
}

func TestBrowserRecoveryRestoresSessionCookiesWithoutReplacingNewerLogin(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	saved := []M{
		{"name": "JSESSIONID", "value": "saved-school", "domain": "learn.tsinghua.edu.cn", "path": "/"},
		{"name": "JSESSIONID", "value": "sso", "domain": "id.tsinghua.edu.cn", "path": "/", "httpOnly": true},
		{"name": "trust", "value": "persistent", "domain": "id.tsinghua.edu.cn", "path": "/", "expires": future},
		{"name": "expired", "value": "old", "domain": "id.tsinghua.edu.cn", "expires": time.Now().Add(-time.Hour).Unix()},
		{"name": "other", "value": "private", "domain": "unrelated.invalid"},
	}
	current := []M{{"name": "JSESSIONID", "value": "newer-school", "domain": "learn.tsinghua.edu.cn", "path": "/"}}
	got := missingBrowserCookies(saved, current)
	if len(got) != 2 || str(got[0], "value") != "sso" || !boolean(got[0], "httpOnly") || number(got[1], "expires", 0) != int(future) {
		t.Fatalf("unexpected cookie selection: %v", got)
	}
}

func TestSessionCookieExpirySurvivesReload(t *testing.T) {
	jar := newJar()
	jar.load(M{"all_cookies": []M{
		{"name": "session", "value": "legacy", "domain": "id.tsinghua.edu.cn"},
		{"name": "expired", "value": "old", "domain": "id.tsinghua.edu.cn", "expires": time.Now().Add(-time.Hour).Unix()},
		{"name": "persistent", "value": "valid", "domain": "id.tsinghua.edu.cn", "expires": time.Now().Add(time.Hour).Unix()},
	}})
	u, _ := url.Parse("https://id.tsinghua.edu.cn/")
	if len(jar.Cookies(u)) != 2 || len(jar.snapshot()) != 2 {
		t.Fatal("expired cookie was restored, or legacy session cookie was lost")
	}
	loaded := newJar()
	loaded.load(M{"all_cookies": jar.snapshot()})
	if len(loaded.Cookies(u)) != 2 {
		t.Fatal("cookie lifetime changed after reload")
	}
	loaded.SetCookies(u, []*http.Cookie{{Name: "renewed", Value: "current", Path: "/", MaxAge: 3600, Expires: time.Now().Add(-time.Hour)}})
	if len(loaded.Cookies(u)) != 3 || len(loaded.snapshot()) != 3 {
		t.Fatal("Max-Age renewal was lost because of an older Expires field")
	}
}
