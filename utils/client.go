package utils

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
)

// proxyLocalBypass 让本机端点（本地测试服务、内网回环）不经过代理。
const proxyLocalBypass = "localhost,127.0.0.1,::1"

var (
	httpClientMu    sync.Mutex
	httpClientCache = make(map[string]*http.Client)
)

// ClientForProxy 返回使用指定代理设置的 HTTP 客户端：空串保持系统默认
// （含 HTTP_PROXY/HTTPS_PROXY 环境变量行为）；否则按配置代理出站。
// 相同设置复用同一实例，避免反复重建连接池。
func ClientForProxy(proxySetting string, timeout time.Duration) (*http.Client, error) {
	proxySetting = strings.TrimSpace(proxySetting)
	transport, err := transportForProxy(proxySetting)
	if err != nil {
		return nil, err
	}

	key := proxySetting + "|" + timeout.String()
	httpClientMu.Lock()
	defer httpClientMu.Unlock()
	if cached, ok := httpClientCache[key]; ok {
		return cached, nil
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	httpClientCache[key] = client
	return client, nil
}

func transportForProxy(proxySetting string) (http.RoundTripper, error) {
	base := func() *http.Transport {
		return &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			IdleConnTimeout:       60 * time.Second,
			MaxIdleConns:          16,
		}
	}

	if proxySetting == "" {
		transport := base()
		transport.Proxy = http.ProxyFromEnvironment
		return transport, nil
	}

	parsed, err := url.Parse(proxySetting)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid proxy URL %q", proxySetting)
	}
	switch parsed.Scheme {
	case "http", "https":
		proxyFunc := (&httpproxy.Config{
			HTTPProxy:  proxySetting,
			HTTPSProxy: proxySetting,
			NoProxy:    proxyLocalBypass,
		}).ProxyFunc()
		transport := base()
		transport.Proxy = func(request *http.Request) (*url.URL, error) {
			return proxyFunc(request.URL)
		}
		return transport, nil
	case "socks5", "socks5h":
		dialer, err := proxy.FromURL(parsed, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("invalid SOCKS proxy: %w", err)
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("SOCKS dialer does not support context")
		}
		transport := base()
		transport.DialContext = contextDialer.DialContext
		return transport, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}
}
