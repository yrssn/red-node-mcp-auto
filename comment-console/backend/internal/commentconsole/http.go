package commentconsole

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func result(c *gin.Context, err error) {
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func bulkDeleteHandler(remove func([]string) (int, int, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			result(c, err)
			return
		}
		deleted, protected, err := remove(body.IDs)
		if err != nil {
			result(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": deleted, "protected": protected})
	}
}

// Register 由入口层统一添加鉴权。
func (e *Engine) Register(g *gin.RouterGroup) {
	g.Use(func(c *gin.Context) {
		if origin := c.GetHeader("Origin"); origin != "" && !consoleOriginAllowed(origin, c.Request.Host) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "不允许来自其他站点的控制台请求"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	g.GET("/state", func(c *gin.Context) { c.JSON(200, e.Snapshot()) })
	g.GET("/ocr/status", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		err := e.ocrClient().Health(ctx)
		message := ""
		if err != nil {
			message = err.Error()
		}
		c.JSON(200, gin.H{"enabled": !strings.EqualFold(os.Getenv("OCR_ENABLED"), "false"), "available": err == nil, "engine": "RapidOCR / ONNX Runtime（本地 CPU）", "error": message})
	})
	g.POST("/posts/:id/reanalyze", func(c *gin.Context) { result(c, e.StartReanalysis([]string{c.Param("id")})) })
	g.POST("/posts/reanalyze", func(c *gin.Context) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			result(c, err)
			return
		}
		result(c, e.StartReanalysis(body.IDs))
	})
	g.POST("/posts/bulk-delete", bulkDeleteHandler(e.DeletePosts))
	g.POST("/comments/bulk-delete", bulkDeleteHandler(e.DeleteComments))
	g.POST("/posts/:id/draft", func(c *gin.Context) {
		var body struct {
			Text string `json:"text"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			result(c, err)
			return
		}
		result(c, e.CreateManualDraft(c.Request.Context(), c.Param("id"), body.Text))
	})
	g.POST("/models", func(c *gin.Context) {
		var v Model
		if err := c.ShouldBindJSON(&v); err != nil {
			result(c, err)
			return
		}
		result(c, e.SaveModel(v))
	})
	g.POST("/strategies", func(c *gin.Context) {
		var v Strategy
		if err := c.ShouldBindJSON(&v); err != nil {
			result(c, err)
			return
		}
		result(c, e.SaveStrategy(v))
	})
	g.POST("/materials", func(c *gin.Context) {
		var v Material
		if err := c.ShouldBindJSON(&v); err != nil {
			result(c, err)
			return
		}
		result(c, e.SaveMaterial(v))
	})
	g.POST("/tasks", func(c *gin.Context) {
		var v Task
		if err := c.ShouldBindJSON(&v); err != nil {
			result(c, err)
			return
		}
		result(c, e.SaveTask(v))
	})
	g.DELETE("/:kind/:id", func(c *gin.Context) { result(c, e.Delete(c.Param("kind"), c.Param("id"))) })
	g.POST("/tasks/:id/:action", func(c *gin.Context) { result(c, e.TaskAction(c.Param("id"), c.Param("action"))) })
	g.POST("/models/:id/test", func(c *gin.Context) { result(c, e.ModelTest(c.Request.Context(), c.Param("id"))) })
	g.POST("/comments/:id/:action", func(c *gin.Context) {
		var body struct {
			Text        string `json:"text"`
			Instruction string `json:"instruction"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			result(c, err)
			return
		}
		if c.Param("action") == "regenerate" {
			result(c, e.Regenerate(c.Request.Context(), c.Param("id"), body.Instruction))
			return
		}
		result(c, e.CommentAction(c.Param("id"), c.Param("action"), body.Text))
	})
	g.POST("/settings", func(c *gin.Context) {
		var v Settings
		if err := c.ShouldBindJSON(&v); err != nil {
			result(c, err)
			return
		}
		if v.SendInterval < 10 || v.SendInterval > 86400 {
			result(c, fmt.Errorf("发送间隔范围为 10–86400 秒"))
			return
		}
		result(c, e.update(func(s *State) error { s.Settings = v; logState(s, "", "info", "更新发送设置"); return nil }))
	})
	g.GET("/account", func(c *gin.Context) {
		a, err := e.VerifyAccountStatus(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, a)
			return
		}
		c.JSON(200, a)
	})
	g.GET("/account/status", func(c *gin.Context) { c.JSON(200, e.AccountStatusSnapshot()) })
	g.GET("/account/qrcode", func(c *gin.Context) {
		e.browserMu.Lock()
		defer e.browserMu.Unlock()
		v, err := e.backend.QRCode(c.Request.Context())
		if err != nil {
			result(c, err)
			return
		}
		e.InvalidateAccountStatus(false)
		c.JSON(200, v)
	})
	g.POST("/account/logout", func(c *gin.Context) {
		if err := e.update(func(s *State) error {
			s.Settings.SendPaused = true
			for i := range s.Tasks {
				s.Tasks[i].Paused = true
			}
			return nil
		}); err != nil {
			result(c, err)
			return
		}
		e.mu.Lock()
		e.cancelTasksLocked()
		e.mu.Unlock()
		e.browserMu.Lock()
		defer e.browserMu.Unlock()
		err := e.backend.Logout(c.Request.Context())
		if err == nil {
			e.InvalidateAccountStatus(true)
		}
		result(c, err)
	})
	g.GET("/export", func(c *gin.Context) {
		s := e.Snapshot()
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="comments-v1.0.0.csv"`)
		_, _ = c.Writer.Write([]byte{0xef, 0xbb, 0xbf})
		w := csv.NewWriter(c.Writer)
		defer w.Flush()
		_ = w.Write([]string{"评论ID", "任务", "帖子标题", "原帖链接", "账号", "状态", "AI原稿", "当前内容", "实际发送", "发送时间", "错误"})
		for _, v := range s.Comments {
			if q := c.Query("status"); !matchesCommentStatus(v.Status, q) {
				continue
			}
			if q := c.Query("task"); q != "" && q != v.TaskID {
				continue
			}
			p := findPost(&s, v.PostID)
			title, link := "", ""
			if p != nil {
				title = p.Title
				link = p.URL
			}
			name := v.TaskID
			if t := findTask(&s, v.TaskID); t != nil {
				name = t.Name
			}
			row := []string{v.ID, name, title, link, v.AccountID, v.Status, v.Original, v.Text, v.SentText, fmt.Sprint(v.SentAt), v.Error}
			for i, x := range row {
				if strings.HasPrefix(strings.TrimSpace(x), "=") || strings.HasPrefix(strings.TrimSpace(x), "+") || strings.HasPrefix(strings.TrimSpace(x), "-") || strings.HasPrefix(strings.TrimSpace(x), "@") {
					row[i] = "'" + x
				}
			}
			_ = w.Write(row)
		}
	})
}

func consoleOriginAllowed(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Host == host {
		return true
	}
	// Vite 仅允许从本机开发地址访问回环服务。
	requestURL, err := url.Parse("http://" + host)
	if err != nil || !contains([]string{"localhost", "127.0.0.1", "::1"}, requestURL.Hostname()) {
		return false
	}
	return contains([]string{"http://localhost:5173", "http://127.0.0.1:5173"}, origin)
}

func matchesCommentStatus(status, filter string) bool {
	switch filter {
	case "", "all":
		return true
	case "review":
		return contains([]string{"review", "draft"}, status)
	case "queued":
		return contains([]string{"queued", "sending"}, status)
	case "issues":
		return contains([]string{"failed", "uncertain"}, status)
	case "skipped":
		return contains([]string{"skipped", "cancelled"}, status)
	default:
		return status == filter
	}
}

type DemoBackend struct{}

func (DemoBackend) Account(context.Context) (Account, error) {
	return Account{ID: "demo-account", Name: "体验账号", LoggedIn: true}, nil
}
func (DemoBackend) QRCode(context.Context) (any, error) {
	return map[string]any{"is_logged_in": true}, nil
}
func (DemoBackend) Logout(context.Context) error {
	return fmt.Errorf("演示账号无需退出；真实账号请使用正式模式")
}
func (DemoBackend) Detail(_ context.Context, p Post) (Post, error) {
	p.Content = "最近准备开始装修，预算有限，想知道应该先确定哪些需求？希望听听大家的建议。"
	return p, nil
}
func (DemoBackend) Send(context.Context, Post, string) error { return nil }
func (DemoBackend) Discover(_ context.Context, source Source, t Task) ([]Post, error) {
	out := []Post{}
	titles := []string{"装修预算怎么分配？想听听大家的建议", "小户型收纳，有哪些实用的设计？", "第一次装修，哪些地方值得多花一点？"}
	for i, title := range titles {
		out = append(out, Post{FeedID: fmt.Sprintf("demo-%d", i), Title: title, Author: "体验用户", URL: "", Token: "demo"})
	}
	return out, nil
}

func (e *Engine) SeedDemo() error {
	if !e.demo {
		return fmt.Errorf("仅演示模式可载入示例")
	}
	return e.update(func(s *State) error {
		if len(s.Tasks) > 0 {
			return nil
		}
		m := Model{ID: id(), Name: "演示模型", BaseURL: "https://example.com/v1", Model: "demo"}
		st := Strategy{ID: id(), Name: "友好交流", Version: 1, Enabled: true, Mode: "ai", MaxLength: 120, Prompt: "结合原帖问题，给出自然、具体、有帮助的建议。"}
		s.Models = append(s.Models, m)
		s.Strategies = append(s.Strategies, st)
		s.Materials = append(s.Materials, Material{ID: id(), Name: "装修交流参考", Category: "知识", Content: "先确认预算、居住人数和功能需求，再讨论具体方案。", Enabled: true})
		t := Task{ID: id(), Name: "装修需求发现", Sources: []Source{{"keyword", "装修预算"}, {"tag", "小户型"}}, ModelID: m.ID, StrategyID: st.ID, Goal: "找到正在咨询装修预算和方案的真实用户，排除广告。", Interval: 300, Limit: 10, SendLimit: 3, Paused: true}
		s.Tasks = append(s.Tasks, t)
		logState(s, t.ID, "info", "演示数据已载入，点击运行一次体验完整流程。")
		return nil
	})
}
