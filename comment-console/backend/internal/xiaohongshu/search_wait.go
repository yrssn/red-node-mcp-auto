package xiaohongshu

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

// 只读取页面已有状态；不要求图片、埋点请求或整个 DOM 停止变化。
const searchFeedsJS = `() => {
	const f = window.__INITIAL_STATE__?.search?.feeds;
	const value = Array.isArray(f) ? f : (f?.value !== undefined ? f.value : f?._value);
	return Array.isArray(value) ? JSON.stringify(value) : "";
}`

func feedIDs(feeds []Feed) string {
	ids := make([]string, 0, len(feeds))
	for _, feed := range feeds {
		ids = append(ids, feed.ID)
	}
	return strings.Join(ids, ",")
}

func readSearchFeeds(page *rod.Page) ([]Feed, error) {
	result, err := page.Eval(searchFeedsJS)
	if err != nil {
		return nil, err
	}
	raw := result.Value.Str()
	if raw == "" {
		return nil, nil
	}
	var feeds []Feed
	if err := json.Unmarshal([]byte(raw), &feeds); err != nil {
		return nil, fmt.Errorf("搜索结果数据格式异常")
	}
	return onlyNotes(feeds), nil
}

func waitSearchResults(page *rod.Page, before string, timeout time.Duration) ([]Feed, error) {
	ctx, cancel := context.WithTimeout(page.GetContext(), timeout)
	defer cancel()
	return pollSearchFeeds(ctx, before, 200*time.Millisecond, func() ([]Feed, error) {
		return readSearchFeeds(page.Context(ctx))
	})
}

// 空数组可能是刷新中间态；无法确认刷新时明确报错，不返回筛选前的数据。
func pollSearchFeeds(ctx context.Context, before string, interval time.Duration, read func() ([]Feed, error)) ([]Feed, error) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		feeds, err := read()
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(feeds) > 0 && (before == "" || feedIDs(feeds) != before) {
			return feeds, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

func waitVisibleSearchElement(page *rod.Page, selector string, timeout time.Duration) (*rod.Element, error) {
	ctx, cancel := context.WithTimeout(page.GetContext(), timeout)
	defer cancel()
	checkPage := page.Context(ctx)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		elements, err := checkPage.Elements(selector)
		if err != nil {
			return nil, err
		}
		for _, element := range elements {
			visible, err := element.Visible()
			if err != nil {
				return nil, err
			}
			if visible {
				// Element.Context 不会替换 Page/Mouse 上下文，必须在父页重新绑定。
				return page.ElementFromObject(element.Object)
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

func searchStageError(ctx context.Context, stage string, err error) error {
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("搜索阶段「%s」失败: %w", stage, ctx.Err())
	case stderrors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("搜索阶段「%s」超时，未确认结果就绪，请检查页面、登录或验证提示: %w", stage, context.DeadlineExceeded)
	case stderrors.Is(err, context.Canceled):
		return fmt.Errorf("搜索阶段「%s」已取消: %w", stage, context.Canceled)
	default:
		return fmt.Errorf("搜索阶段「%s」失败，请检查页面结构或浏览器状态", stage)
	}
}
