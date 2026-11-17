package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const localOCRURL = "http://127.0.0.1:18063"

func startLocalOCR(ctx context.Context) (func(), error) {
	if endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("OCR_URL")), "/"); endpoint != "" && endpoint != localOCRURL {
		return nil, fmt.Errorf("-local-ocr 使用 %s，请清除自定义 OCR_URL 后重试", localOCRURL)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OCR_ENABLED")), "false") {
		return nil, fmt.Errorf("本地 OCR 已禁用，请清除 OCR_ENABLED=false 后重试")
	}
	client := &http.Client{
		Timeout:   time.Second,
		Transport: &http.Transport{},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()
	if localOCRReady(ctx, client) {
		logrus.Infof("复用本地 OCR：%s", localOCRURL)
		return func() {}, nil
	}
	python, err := filepath.Abs("../.runtime/ocr/Scripts/python.exe")
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(python); err != nil {
		return nil, fmt.Errorf("找不到 OCR 的 Python 环境，请在 backend 目录运行并先按 ocr/README.md 安装")
	}
	cmd := exec.Command(python, "-u", "../ocr/server.py")
	cmd.Env = append(os.Environ(), "BIND=127.0.0.1", "PORT=18063", "PYTHONDONTWRITEBYTECODE=1")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动本地 OCR：%w", err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()
	stop := func() {
		_ = cmd.Process.Kill()
		<-done
	}
	startup, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return nil, fmt.Errorf("本地 OCR 在就绪前退出（%v），请查看上方 Python 日志", waitErr)
		case <-startup.Done():
			stop()
			return nil, fmt.Errorf("等待本地 OCR 就绪：%w", startup.Err())
		case <-tick.C:
			if localOCRReady(startup, client) {
				logrus.Infof("本地 OCR 已就绪：%s", localOCRURL)
				return stop, nil
			}
		}
	}
}

func localOCRReady(ctx context.Context, client *http.Client) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, localOCRURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var health struct {
		OK     bool   `json:"ok"`
		Engine string `json:"engine"`
	}
	return resp.StatusCode == http.StatusOK &&
		json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&health) == nil &&
		health.OK && strings.Contains(strings.ToLower(health.Engine), "rapidocr")
}
