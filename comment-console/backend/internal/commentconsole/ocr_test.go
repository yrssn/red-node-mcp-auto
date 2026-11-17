package commentconsole

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type ocrRoundTripFunc func(*http.Request) (*http.Response, error)

func (f ocrRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type ocrLookupFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f ocrLookupFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func ocrPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func ocrResponse(request *http.Request, status int, data []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Request: request}
}

func mockOCRClient(t *testing.T, data []byte, handler http.HandlerFunc) *httpOCRClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	images := newOCRImageClient(net.DefaultResolver, (&net.Dialer{}).DialContext)
	images.Transport = ocrRoundTripFunc(func(r *http.Request) (*http.Response, error) { return ocrResponse(r, 200, data), nil })
	return &httpOCRClient{endpoint: server.URL, images: images, service: server.Client(), cache: map[[32]byte]string{}}
}

func TestOCRURLPolicy(t *testing.T) {
	for _, source := range []string{"https://sns-webpic-qc.xhscdn.com/a?sign=private", "https://ci.xiaohongshu.com/a", "https://xhscdn.com:443/a"} {
		if !IsAllowedPostImageURL(source) {
			t.Errorf("可信HTTPS图片被拒绝：%s", source)
		}
	}
	for _, source := range []string{
		"http://sns-webpic-qc.xhscdn.com/a", "ftp://xhscdn.com/a", "//xhscdn.com/a",
		"https://xhscdn.com.example.org/a", "https://fake-xhscdn.com/a", "https://example.com/a",
		"https://127.0.0.1/a", "https://localhost/a", "https://xhscdn.com:8443/a", "https://user:password@xhscdn.com/a",
	} {
		if IsAllowedPostImageURL(source) {
			t.Errorf("不可信图片地址被允许：%s", source)
		}
	}
}

func TestOCRDNSRejectsPrivateAndMixedAnswers(t *testing.T) {
	answers := [][]string{
		{"127.0.0.1"}, {"10.0.0.1"}, {"172.16.1.1"}, {"192.168.1.1"}, {"169.254.169.254"}, {"100.64.0.1"},
		{"::1"}, {"fd00::1"}, {"::ffff:127.0.0.1"}, {"1.1.1.1", "10.0.0.1"}, {"198.51.100.1"}, {"fec0::1"},
		{"198.18.0.113"}, {"198.19.255.254"},
	}
	for _, values := range answers {
		t.Run(strings.Join(values, ","), func(t *testing.T) {
			var calls atomic.Int32
			lookup := ocrLookupFunc(func(context.Context, string, string) ([]netip.Addr, error) {
				out := []netip.Addr{}
				for _, v := range values {
					out = append(out, netip.MustParseAddr(v))
				}
				return out, nil
			})
			client := newOCRImageClient(lookup, func(context.Context, string, string) (net.Conn, error) {
				calls.Add(1)
				return nil, errors.New("unexpected dial")
			})
			_, err := client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "sns-webpic-qc.xhscdn.com:443")
			if err == nil || calls.Load() != 0 {
				t.Fatal("非公网DNS结果触发了网络连接")
			}
		})
	}
}

func TestOCRFakeIPOptInIsLimitedToBenchmarkCDNAddresses(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		addresses []string
		allowDial bool
	}{
		{"TUN映射", "sns-webpic-qc.xhscdn.com", []string{"198.18.0.113"}, true},
		{"benchmark另一半", "ci.xiaohongshu.com", []string{"198.19.255.254"}, true},
		{"LAN", "sns-webpic-qc.xhscdn.com", []string{"192.168.1.1"}, false},
		{"私网", "sns-webpic-qc.xhscdn.com", []string{"10.0.0.1"}, false},
		{"loopback", "sns-webpic-qc.xhscdn.com", []string{"127.0.0.1"}, false},
		{"metadata", "sns-webpic-qc.xhscdn.com", []string{"169.254.169.254"}, false},
		{"shared", "sns-webpic-qc.xhscdn.com", []string{"100.64.0.1"}, false},
		{"文档网段", "sns-webpic-qc.xhscdn.com", []string{"198.51.100.1"}, false},
		{"混合私网结果", "sns-webpic-qc.xhscdn.com", []string{"198.18.0.113", "10.0.0.1"}, false},
		{"其他域名", "example.com", []string{"198.18.0.113"}, false},
		{"伪造CDN", "xhscdn.com.example.com", []string{"198.18.0.113"}, false},
		{"直接IP", "198.18.0.113", []string{"198.18.0.113"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialed := ""
			resolver := ocrLookupFunc(func(context.Context, string, string) ([]netip.Addr, error) {
				var addresses []netip.Addr
				for _, address := range tt.addresses {
					addresses = append(addresses, netip.MustParseAddr(address))
				}
				return addresses, nil
			})
			client := newOCRImageClient(resolver, func(_ context.Context, _, address string) (net.Conn, error) {
				dialed = address
				return nil, errors.New("test dial failure")
			}, true)
			_, _ = client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", net.JoinHostPort(tt.host, "443"))
			if (dialed != "") != tt.allowDial {
				t.Fatalf("fake-IP开关错误放行或拒绝：dialed=%q", dialed)
			}
			if tt.allowDial && dialed != net.JoinHostPort(tt.addresses[0], "443") {
				t.Fatal("连接未固定到已验证的DNS映射IP")
			}
			transport := client.Transport.(*http.Transport)
			if transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
				t.Fatal("fake-IP开关不得关闭TLS验证")
			}
		})
	}
}

func TestOCRFakeIPStillVerifiesTLSHostname(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)
	resolver := ocrLookupFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("198.18.0.113")}, nil
	})
	dialed := ""
	client := newOCRImageClient(resolver, func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = address
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}, true)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	// 信任测试证书的签发者，但它不匹配请求的 CDN 主机名。
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	response, err := client.Get("https://sns-webpic-qc.xhscdn.com/image")
	if response != nil {
		response.Body.Close()
	}
	if err == nil || requests.Load() != 0 || dialed != "198.18.0.113:443" {
		t.Fatalf("fake-IP开关绕过了TLS主机名验证：err=%v requests=%d dialed=%s", err, requests.Load(), dialed)
	}
}

func TestOCRDialPinsValidatedPublicIP(t *testing.T) {
	lookups := 0
	dialed := ""
	lookup := ocrLookupFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})
	client := newOCRImageClient(lookup, func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = address
		return nil, errors.New("test dial failure")
	})
	_, _ = client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "sns-webpic-qc.xhscdn.com:443")
	if lookups != 1 || dialed != "1.1.1.1:443" {
		t.Fatalf("DNS校验后未使用同一公网IP连接：lookups=%d address=%s", lookups, dialed)
	}
}

func TestOCRRawBytesAndHashCacheAcrossSignedURLs(t *testing.T) {
	data := ocrPNG(t)
	var calls atomic.Int32
	client := mockOCRClient(t, data, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/ocr" || r.Header.Get("Content-Type") != "application/octet-stream" || !bytes.Equal(body, data) {
			t.Error("OCR服务未收到原始图片字节")
		}
		_, _ = io.WriteString(w, "{\"text\":\"预算三千，希望找一个桌面软件开发人员。\"}")
	})
	for _, source := range []string{"https://sns-webpic-qc.xhscdn.com/a?sign=one", "https://sns-webpic-qc.xhscdn.com/a?sign=two"} {
		result := client.RecognizePost(context.Background(), []string{source})
		if result.Status != "success" || !strings.Contains(result.Text, "预算三千") || result.Error != "" {
			t.Fatalf("识别失败：%+v", result)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("相同图片的不同签名URL未命中hash缓存：%d", calls.Load())
	}
}

func TestOCRRedirectsAreValidatedAndCredentialsNeverForwarded(t *testing.T) {
	for _, target := range []string{"https://example.org/a?private-sign=redirect", "http://xhscdn.com/a"} {
		t.Run(target, func(t *testing.T) {
			client := mockOCRClient(t, ocrPNG(t), func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用OCR"); w.WriteHeader(500) })
			var calls atomic.Int32
			client.images.Transport = ocrRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				response := ocrResponse(r, http.StatusFound, nil)
				response.Header.Set("Location", target)
				return response, nil
			})
			result := client.RecognizePost(context.Background(), []string{"https://sns-webpic-qc.xhscdn.com/a?private-sign=original"})
			if result.Status != "failed" || calls.Load() != 1 {
				t.Fatalf("未阻止跳转：%+v calls=%d", result, calls.Load())
			}
			if strings.Contains(result.Error, "private-sign") || strings.Contains(result.Error, "https://") {
				t.Fatal("错误信息泄漏签名URL")
			}
		})
	}
	t.Run("可信域名间跳转", func(t *testing.T) {
		data := ocrPNG(t)
		client := mockOCRClient(t, data, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "{\"text\":\"测试文字\"}") })
		var calls atomic.Int32
		client.images.Transport = ocrRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				response := ocrResponse(r, 302, nil)
				response.Header.Set("Location", "https://safe.xhscdn.com/final")
				response.Header.Set("Set-Cookie", "session=private")
				return response, nil
			}
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "" {
				t.Error("图片下载发送了凭证或签名来源")
			}
			return ocrResponse(r, 200, data), nil
		})
		result := client.RecognizePost(context.Background(), []string{"https://sns-webpic-qc.xhscdn.com/a?private-sign=original"})
		if result.Status != "success" || calls.Load() != 2 {
			t.Fatalf("可信跳转失败：%+v", result)
		}
	})
}

func TestOCRLimitsAndPartialResult(t *testing.T) {
	data := ocrPNG(t)
	client := mockOCRClient(t, data, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{\"text\":\"可识别文字\"}")
	})
	var downloads atomic.Int32
	client.images.Transport = ocrRoundTripFunc(func(r *http.Request) (*http.Response, error) { downloads.Add(1); return ocrResponse(r, 200, data), nil })
	result := client.RecognizePost(context.Background(), []string{"https://a.xhscdn.com/1", "https://a.xhscdn.com/2", "https://a.xhscdn.com/3", "https://a.xhscdn.com/4"})
	if result.Status != "success" || downloads.Load() != 4 {
		t.Fatalf("应下载全部四张图片：%+v downloads=%d", result, downloads.Load())
	}
	downloads.Store(0)
	result = client.RecognizePost(context.Background(), []string{"https://a.xhscdn.com/1", "https://a.xhscdn.com/2", "https://a.xhscdn.com/3", "https://a.xhscdn.com/3"})
	if result.Status != "success" || downloads.Load() != 3 {
		t.Fatalf("重复URL不应触发截断警告：%+v downloads=%d", result, downloads.Load())
	}
	result = client.RecognizePost(context.Background(), []string{"https://outside.example/a?secret=hidden", "https://a.xhscdn.com/valid"})
	if result.Status != "partial" || result.Text == "" || result.Error == "" {
		t.Fatalf("部分失败应保留成功文字：%+v", result)
	}
}

func TestOCRRejectsOversizedOrInvalidImagesBeforeService(t *testing.T) {
	data := ocrPNG(t)
	oversizedPixels := append([]byte{}, data...)
	binary.BigEndian.PutUint32(oversizedPixels[16:20], 4001)
	binary.BigEndian.PutUint32(oversizedPixels[20:24], 4000)
	binary.BigEndian.PutUint32(oversizedPixels[29:33], crc32.ChecksumIEEE(oversizedPixels[12:29]))
	for name, fixture := range map[string][]byte{"非图片": []byte("not an image"), "超像素": oversizedPixels, "超字节": make([]byte, maxOCRImageBytes+1)} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			client := mockOCRClient(t, fixture, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
			result := client.RecognizePost(context.Background(), []string{"https://a.xhscdn.com/image?private-sign=hidden"})
			if result.Status != "failed" || calls.Load() != 0 {
				t.Fatalf("无效图片进入了OCR服务：%+v", result)
			}
			if strings.Contains(result.Error, "private-sign") || strings.Contains(result.Error, "https://") {
				t.Fatal("错误信息泄漏图片URL")
			}
		})
	}
}

func TestOCRFailuresAreNotCached(t *testing.T) {
	var calls atomic.Int32
	client := mockOCRClient(t, ocrPNG(t), func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "do not expose this body", 500)
			return
		}
		_, _ = io.WriteString(w, "{\"text\":\"重试后识别成功\"}")
	})
	images := []string{"https://a.xhscdn.com/image"}
	if first := client.RecognizePost(context.Background(), images); first.Status != "failed" || strings.Contains(first.Error, "expose") {
		t.Fatalf("失败结果不正确：%+v", first)
	}
	if second := client.RecognizePost(context.Background(), images); second.Status != "success" || calls.Load() != 2 {
		t.Fatalf("失败结果不应缓存：%+v calls=%d", second, calls.Load())
	}
}

func TestOCRCacheIsBounded(t *testing.T) {
	client := &httpOCRClient{cache: map[[32]byte]string{}}
	first := sha256.Sum256([]byte("first"))
	client.remember(first, "第一张")
	for i := 0; i < maxOCRCacheEntries; i++ {
		client.remember(sha256.Sum256([]byte{byte(i), 1}), "已识别")
	}
	if len(client.cache) > maxOCRCacheEntries {
		t.Fatal("OCR缓存超过256项")
	}
	if _, exists := client.cached(first); exists {
		t.Fatal("最旧OCR缓存未被淘汰")
	}
}

func TestOCRCanBeCancelledDuringServiceRequest(t *testing.T) {
	entered := make(chan struct{})
	client := mockOCRClient(t, ocrPNG(t), func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan OCRResult, 1)
	go func() { finished <- client.RecognizePost(ctx, []string{"https://a.xhscdn.com/image"}) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("OCR请求未到达mock服务")
	}
	cancel()
	select {
	case result := <-finished:
		if result.Status != "failed" || !strings.Contains(result.Error, "取消") {
			t.Fatalf("取消未生效：%+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OCR未响应取消")
	}
}

func TestOCREnvironmentAndHealth(t *testing.T) {
	t.Setenv("OCR_ENABLED", "false")
	t.Setenv("OCR_URL", "invalid-url")
	client := NewOCRClientFromEnv()
	if result := client.RecognizePost(context.Background(), []string{"https://a.xhscdn.com/image"}); result.Status != "disabled" {
		t.Fatalf("禁用未生效：%+v", result)
	}
	t.Setenv("OCR_ENABLED", "true")
	client = NewOCRClientFromEnv()
	if err := client.Health(context.Background()); err == nil || strings.Contains(err.Error(), "invalid-url") {
		t.Fatal("错误配置未安全拒绝")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Error("健康检查路径无效")
		}
		_, _ = io.WriteString(w, "{\"ok\":true}")
	}))
	t.Cleanup(server.Close)
	t.Setenv("OCR_URL", server.URL)
	client = NewOCRClientFromEnv()
	if err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}
