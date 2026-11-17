package commentconsole

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"
)

const (
	maxOCRImageBytes   = 10 << 20
	maxOCRImagePixels  = 16000000
	maxOCRCacheEntries = 256
	maxOCRTextRunes    = 12000
)

type OCRResult struct {
	Status string
	Text   string
	Error  string
}

type OCRClient interface {
	RecognizePost(context.Context, []string) OCRResult
	Health(context.Context) error
}

type ocrResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type ocrDialFunc func(context.Context, string, string) (net.Conn, error)

type httpOCRClient struct {
	endpoint    string
	disabled    bool
	configError bool
	images      *http.Client
	service     *http.Client
	mu          sync.Mutex
	cache       map[[32]byte]string
	order       [][32]byte
}

// NewOCRClientFromEnv 仅本地服务接收图片字节，外部图片由 Go 下载并校验。
func NewOCRClientFromEnv() OCRClient {
	endpoint := strings.TrimSpace(os.Getenv("OCR_URL"))
	if endpoint == "" {
		endpoint = "http://127.0.0.1:18063"
	}
	u, err := url.Parse(endpoint)
	invalid := err != nil || u == nil
	if !invalid {
		invalid = (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != ""
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	return &httpOCRClient{
		endpoint:    strings.TrimRight(endpoint, "/"),
		disabled:    strings.EqualFold(strings.TrimSpace(os.Getenv("OCR_ENABLED")), "false"),
		configError: invalid,
		images:      newOCRImageClient(net.DefaultResolver, dialer.DialContext, strings.EqualFold(strings.TrimSpace(os.Getenv("OCR_ALLOW_FAKE_IP")), "true")),
		service:     &http.Client{Timeout: 35 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: dialer.DialContext, ResponseHeaderTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		cache:       map[[32]byte]string{},
	}
}

func IsAllowedPostImageURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "xiaohongshu.com" || strings.HasSuffix(host, ".xiaohongshu.com") || host == "xhscdn.com" || strings.HasSuffix(host, ".xhscdn.com")
}

var blockedOCRNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

var ocrFakeIPNetwork = netip.MustParsePrefix("198.18.0.0/15")

func publicOCRAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.Is6() && !netip.MustParsePrefix("2000::/3").Contains(address) {
		return false
	}
	for _, prefix := range blockedOCRNetworks {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

// DNS 校验与连接使用同一组 IP，避免校验后重解析造成 DNS rebinding。
func newOCRImageClient(resolver ocrResolver, dial ocrDialFunc, allowFakeIP ...bool) *http.Client {
	// 仅兼容本地 TUN 的 benchmark 映射；域名与 TLS 校验仍然生效。
	fakeIPEnabled := len(allowFakeIP) > 0 && allowFakeIP[0]
	transport := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 8 * time.Second, MaxIdleConns: 8, IdleConnTimeout: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" || !IsAllowedPostImageURL("https://"+net.JoinHostPort(host, port)+"/") {
			return nil, errors.New("图片下载目标不受信任")
		}
		addresses, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("图片域名解析失败")
		}
		for _, ip := range addresses {
			if !publicOCRAddress(ip) && !(fakeIPEnabled && ocrFakeIPNetwork.Contains(ip.Unmap())) {
				return nil, errors.New("图片域名指向非公网地址")
			}
		}
		for _, ip := range addresses {
			connection, err := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			if err == nil {
				return connection, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errors.New("图片服务器连接失败")
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 4 || !IsAllowedPostImageURL(request.URL.String()) {
			return errors.New("图片跳转目标不受信任")
		}
		request.Header.Del("Referer")
		request.Header.Del("Cookie")
		request.Header.Del("Authorization")
		return nil
	}}
}

func (c *httpOCRClient) Health(ctx context.Context) error {
	if c.disabled {
		return errors.New("本地 OCR 已禁用")
	}
	if c.configError {
		return errors.New("本地 OCR 服务地址配置无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return errors.New("本地 OCR 服务地址配置无效")
	}
	response, err := c.service.Do(request)
	if err != nil {
		return errors.New("本地 OCR 服务不可用")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("本地 OCR 服务未就绪")
	}
	return nil
}

func (c *httpOCRClient) RecognizePost(ctx context.Context, images []string) OCRResult {
	if c.disabled {
		return OCRResult{Status: "disabled"}
	}
	if c.configError {
		return OCRResult{Status: "failed", Error: "本地 OCR 服务地址配置无效"}
	}
	if len(images) == 0 {
		return OCRResult{Status: "empty"}
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	texts := []string{}
	successes, failures, attempted := 0, 0, 0
	lastError := ""
	seenURL := map[string]bool{}
	seenData := map[[32]byte]bool{}
	for _, source := range images {
		if seenURL[source] {
			continue
		}
		seenURL[source] = true
		attempted++
		if ctx.Err() != nil {
			failures++
			lastError = ocrContextError(ctx)
			break
		}
		data, err := c.download(ctx, source)
		if err != nil {
			failures++
			lastError = err.Error()
			continue
		}
		hash := sha256.Sum256(data)
		if seenData[hash] {
			continue
		}
		seenData[hash] = true
		text, cached := c.cached(hash)
		if !cached {
			text, err = c.recognize(ctx, data)
			if err != nil {
				failures++
				lastError = err.Error()
				continue
			}
			c.remember(hash, text)
		}
		successes++
		if text != "" {
			texts = append(texts, fmt.Sprintf("图片%d：\n%s", attempted, text))
		}
	}
	result := OCRResult{Status: "success", Text: strings.Join(texts, "\n\n")}
	if failures > 0 {
		result.Error = lastError
		if successes == 0 {
			result.Status = "failed"
		} else {
			result.Status = "partial"
		}
	}
	if failures == 0 && result.Text == "" {
		result.Status = "empty"
	}
	return result
}

func ocrContextError(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "图片文字识别已取消"
	}
	return "图片文字识别超时"
}

func (c *httpOCRClient) download(ctx context.Context, source string) ([]byte, error) {
	if !IsAllowedPostImageURL(source) {
		return nil, errors.New("图片 URL 不在可信 HTTPS 域名范围")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, errors.New("图片地址格式无效")
	}
	request.Header.Set("Accept", "image/png,image/jpeg,image/webp")
	response, err := c.images.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New(ocrContextError(ctx))
		}
		return nil, errors.New("图片下载失败或跳转被拒绝")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("图片服务器返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxOCRImageBytes {
		return nil, errors.New("图片超过 10 MiB 上限")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxOCRImageBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New(ocrContextError(ctx))
		}
		return nil, errors.New("图片读取失败")
	}
	if len(data) > maxOCRImageBytes {
		return nil, errors.New("图片超过 10 MiB 上限")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") || config.Width <= 0 || config.Height <= 0 || config.Width > maxOCRImagePixels || config.Height > maxOCRImagePixels || int64(config.Width)*int64(config.Height) > maxOCRImagePixels {
		return nil, errors.New("图片格式无效或超过 1600 万像素")
	}
	return data, nil
}

func (c *httpOCRClient) recognize(ctx context.Context, data []byte) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/ocr", bytes.NewReader(data))
	if err != nil {
		return "", errors.New("本地 OCR 服务地址配置无效")
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := c.service.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New(ocrContextError(ctx))
		}
		return "", errors.New("本地 OCR 服务不可用")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("本地 OCR 服务返回 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (128<<10)+1))
	if err != nil || len(body) > 128<<10 {
		return "", errors.New("本地 OCR 响应无效或过大")
	}
	var result struct {
		Text *string `json:"text"`
	}
	if err = json.Unmarshal(body, &result); err != nil || result.Text == nil {
		return "", errors.New("本地 OCR 响应格式无效")
	}
	text := strings.TrimSpace(*result.Text)
	runes := []rune(text)
	if len(runes) > maxOCRTextRunes {
		text = string(runes[:maxOCRTextRunes])
	}
	return text, nil
}

func (c *httpOCRClient) cached(hash [32]byte) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	text, ok := c.cache[hash]
	return text, ok
}
func (c *httpOCRClient) remember(hash [32]byte, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[[32]byte]string{}
	}
	if _, exists := c.cache[hash]; exists {
		c.cache[hash] = text
		return
	}
	if len(c.order) >= maxOCRCacheEntries {
		delete(c.cache, c.order[0])
		c.order = c.order[1:]
	}
	c.cache[hash] = text
	c.order = append(c.order, hash)
}
