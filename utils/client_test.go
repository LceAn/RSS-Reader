package utils

import (
	"testing"
	"time"
)

func TestClientForProxyDirect(t *testing.T) {
	client, err := ClientForProxy("", 0)
	if err != nil {
		t.Fatalf("直连客户端构建失败: %v", err)
	}
	if client.Timeout != 0 {
		t.Fatalf("直连超时 = %v, 期望 0", client.Timeout)
	}
	// 相同设置应复用缓存实例。
	again, _ := ClientForProxy(" ", 0)
	if again != client {
		t.Fatal("相同代理设置应复用客户端实例")
	}
}

func TestClientForProxyHTTPAndSOCKS(t *testing.T) {
	for _, setting := range []string{
		"http://127.0.0.1:7890",
		"https://proxy.example.com:8443",
		"socks5://127.0.0.1:1080",
		"socks5h://127.0.0.1:1080",
	} {
		if _, err := ClientForProxy(setting, 15*time.Second); err != nil {
			t.Fatalf("代理 %q 构建失败: %v", setting, err)
		}
	}
}

func TestClientForProxyRejectsInvalid(t *testing.T) {
	for _, setting := range []string{
		"garbage",
		"ftp://127.0.0.1:21",
		"http://",
	} {
		if _, err := ClientForProxy(setting, 0); err == nil {
			t.Fatalf("无效代理 %q 不应构建成功", setting)
		}
	}
}
