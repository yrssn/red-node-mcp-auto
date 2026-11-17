package xiaohongshu

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"xhs-comment-console/internal/humanize"
)

func TestSearchWaitsForActualFeedsInsteadOfInitialState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reads := 0
	feeds, err := pollSearchFeeds(ctx, "", time.Millisecond, func() ([]Feed, error) {
		reads++
		if reads < 3 {
			return []Feed{}, nil
		}
		return []Feed{{ID: "new-note", ModelType: modelTypeNote}}, nil
	})
	if err != nil || reads != 3 || len(feeds) != 1 || feeds[0].ID != "new-note" {
		t.Fatalf("未等待搜索结果就绪: reads=%d feeds=%v err=%v", reads, feeds, err)
	}
}

func TestSearchFilterWaitRejectsUnconfirmedOldOrEmptyResults(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "相同结果"
		if empty {
			name = "刷新中空数组"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			feeds, err := pollSearchFeeds(ctx, "old-note", time.Millisecond, func() ([]Feed, error) {
				if empty {
					return []Feed{}, nil
				}
				return []Feed{{ID: "old-note"}}, nil
			})
			if !errors.Is(err, context.DeadlineExceeded) || feeds != nil {
				t.Fatalf("未确认刷新时返回了旧结果: feeds=%v err=%v", feeds, err)
			}
		})
	}
}

func TestSearchFilterWaitAcceptsNewBatchAfterClear(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reads := 0
	feeds, err := pollSearchFeeds(ctx, "old-note", time.Millisecond, func() ([]Feed, error) {
		reads++
		switch reads {
		case 1:
			return []Feed{{ID: "old-note"}}, nil
		case 2:
			return []Feed{}, nil
		default:
			return []Feed{{ID: "new-note"}}, nil
		}
	})
	if err != nil || reads != 3 || len(feeds) != 1 || feeds[0].ID != "new-note" {
		t.Fatalf("未等待新批次: reads=%d feeds=%v err=%v", reads, feeds, err)
	}
}

func TestSearchWaitHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reads := 0
	_, err := pollSearchFeeds(ctx, "", time.Second, func() ([]Feed, error) { reads++; return nil, nil })
	if !errors.Is(err, context.Canceled) || reads != 0 {
		t.Fatalf("取消后仍读取页面: reads=%d err=%v", reads, err)
	}
}

func TestSearchStageErrorKeepsCauseWithoutLeakingPageDetails(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		err := searchStageError(context.Background(), "等待首屏搜索结果", cause)
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "等待首屏搜索结果") {
			t.Fatalf("阶段或超时原因丢失: %v", err)
		}
	}
	err := searchStageError(context.Background(), "导航搜索页", errors.New("https://example.test/?token=private-token Cookie=private-cookie"))
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "https://") {
		t.Fatal("页面原始错误泄露到接口")
	}
}

func TestSearchRejectsInvalidFiltersBeforeUsingPage(t *testing.T) {
	_, err := NewSearchAction(nil).Search(context.Background(), "测试", FilterOption{SortBy: "非法选项"})
	if err == nil || !strings.Contains(err.Error(), "排序依据") {
		t.Fatalf("没有在导航前校验筛选条件: %v", err)
	}
}

// 指定已安装的浏览器运行本地 DOM 回归；不下载、不读取 Cookie、不访问站点。
func TestSearchVisibleElementKeepsParentPageContext(t *testing.T) {
	bin := os.Getenv("TEST_SEARCH_BROWSER_BIN")
	if bin == "" {
		t.Skip("未设置 TEST_SEARCH_BROWSER_BIN，跳过本地浏览器 DOM 回归")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	l := launcher.New().Context(ctx).Bin(bin).Headless(true).Leakless(false)
	endpoint, err := l.Launch()
	if err != nil {
		t.Fatal(err)
	}
	b := rod.New().Context(ctx).ControlURL(endpoint)
	t.Cleanup(func() { _ = b.Close(); l.Kill(); l.Cleanup() })
	if err := b.Connect(); err != nil {
		t.Fatal(err)
	}
	page, err := b.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.SetDocumentContent(`<button class="filter" style="width:120px;height:50px">筛选</button>`); err != nil {
		t.Fatal(err)
	}
	element, err := waitVisibleSearchElement(page, "button.filter", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := element.GetContext().Err(); err != nil {
		t.Fatalf("返回元素的上下文已取消: %v", err)
	}
	if err := element.Page().GetContext().Err(); err != nil {
		t.Fatalf("返回元素所属页面的上下文已取消: %v", err)
	}
	if err := humanize.Hover(element); err != nil {
		t.Fatalf("等待结束后的鼠标悬停失败: %v", err)
	}
}
