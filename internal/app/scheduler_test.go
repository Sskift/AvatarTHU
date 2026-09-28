package app

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

func TestSyncFailureOnlyRequestsLoginForAuthenticationErrors(t *testing.T) {
	for _, err := range []error{
		io.ErrUnexpectedEOF,
		fmt.Errorf("读取附件失败: %w", io.ErrUnexpectedEOF),
		io.EOF,
		&net.DNSError{Err: "no such host", Name: "learn.tsinghua.edu.cn"},
		fmt.Errorf("下载 login.pdf 失败"),
	} {
		message := syncFailureMessage(err)
		if strings.Contains(message, "avatarthu login thu") || strings.Contains(message, "重新认证") {
			t.Fatalf("non-authentication error requested login: %v: %s", err, message)
		}
		if !strings.Contains(message, "15 分钟后自动重试") {
			t.Fatalf("missing retry guidance: %s", message)
		}
	}
	for _, err := range []error{
		sessionExpired{"登录过期"},
		fmt.Errorf("请求失败: %w", sessionExpired{"登录过期"}),
	} {
		if message := syncFailureMessage(err); !strings.Contains(message, "avatarthu login thu") {
			t.Fatalf("authentication error omitted login guidance: %s", message)
		}
	}
}
