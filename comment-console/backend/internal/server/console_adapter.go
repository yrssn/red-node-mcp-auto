package server

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	cc "xhs-comment-console/internal/commentconsole"
	"xhs-comment-console/internal/xiaohongshu"
)

type consoleBackend struct{ service *XiaohongshuService }

func (b consoleBackend) Account(ctx context.Context) (cc.Account, error) {
	v, err := b.service.CheckLoginStatus(ctx)
	if err != nil {
		return cc.Account{}, err
	}
	return cc.Account{ID: v.UserID, Name: v.Username, LoggedIn: v.IsLoggedIn}, nil
}
func (b consoleBackend) QRCode(ctx context.Context) (any, error) {
	return b.service.GetLoginQrcode(ctx)
}
func (b consoleBackend) Logout(ctx context.Context) error { return b.service.DeleteCookies(ctx) }
func (b consoleBackend) Send(ctx context.Context, p cc.Post, text string) error {
	_, err := b.service.PostCommentToFeed(ctx, p.FeedID, p.Token, text)
	return err
}
func (b consoleBackend) Detail(ctx context.Context, p cc.Post) (cc.Post, error) {
	v, err := b.service.GetFeedDetail(ctx, p.FeedID, p.Token, false)
	if err != nil {
		return p, err
	}
	d, ok := v.Data.(*xiaohongshu.FeedDetailResponse)
	if !ok {
		return p, fmt.Errorf("详情数据格式无效")
	}
	p.Title = d.Note.Title
	p.Content = d.Note.Desc
	p.Author = d.Note.User.Nickname
	if p.Author == "" {
		p.Author = d.Note.User.NickName
	}
	p.Images = []string{}
	seenImages := map[string]bool{}
	for _, picture := range d.Note.ImageList {
		found := false
		for _, candidate := range []string{picture.URLDefault, picture.URLPre} {
			candidate = strings.TrimSpace(candidate)
			if strings.HasPrefix(candidate, "//") {
				candidate = "https:" + candidate
			}
			if u, err := url.Parse(candidate); err == nil && u.Scheme == "http" {
				u.Scheme = "https"
				candidate = u.String()
			}
			if !cc.IsAllowedPostImageURL(candidate) {
				continue
			}
			found = true
			if !seenImages[candidate] {
				p.Images = append(p.Images, candidate)
				seenImages[candidate] = true
			}
			break
		}
		if !found {
			// 保留缺图标记，让 OCR 将不完整的帖子标为待核对。
			p.Images = append(p.Images, "")
		}
	}
	p.AuthorUpdates = loadedAuthorUpdates(d.Comments.List, d.Note.User.UserID)
	return p, nil
}

// 只保留已加载的作者发言，不把其他用户评论当成原帖需求。
func loadedAuthorUpdates(comments []xiaohongshu.Comment, authorID string) string {
	if authorID == "" {
		return ""
	}
	updates := make([]string, 0, 8)
	seen := map[string]bool{}
	var visit func([]xiaohongshu.Comment)
	visit = func(list []xiaohongshu.Comment) {
		for _, comment := range list {
			if len(updates) >= 8 {
				return
			}
			text := strings.TrimSpace(comment.Content)
			if comment.UserInfo.UserID == authorID && text != "" && !seen[text] {
				seen[text] = true
				runes := []rune(text)
				if len(runes) > 500 {
					text = string(runes[:500])
				}
				updates = append(updates, text)
			}
			visit(comment.SubComments)
		}
	}
	visit(comments)
	return strings.Join(updates, "\n")
}

func parseConsoleURL(raw, kind string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || (u.Host != "www.xiaohongshu.com" && u.Host != "xiaohongshu.com") {
		return "", "", fmt.Errorf("请输入完整的小红书 HTTPS 链接，短链接请先在浏览器展开")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if kind == "profile" && len(parts) == 3 && parts[0] == "user" && parts[1] == "profile" && parts[2] != "" {
		return parts[2], u.Query().Get("xsec_token"), nil
	}
	if kind == "link" && len(parts) == 2 && (parts[0] == "explore" || parts[0] == "discovery") && parts[1] != "" {
		return parts[1], u.Query().Get("xsec_token"), nil
	}
	if kind == "link" && len(parts) == 3 && parts[0] == "discovery" && parts[1] == "item" && parts[2] != "" {
		return parts[2], u.Query().Get("xsec_token"), nil
	}
	return "", "", fmt.Errorf("链接路径不匹配，请使用用户主页或帖子详情链接")
}

func (b consoleBackend) Discover(ctx context.Context, src cc.Source, t cc.Task) ([]cc.Post, error) {
	var feeds []xiaohongshu.Feed
	switch src.Kind {
	case "keyword", "tag":
		keyword := strings.TrimSpace(src.Value)
		if src.Kind == "tag" {
			keyword = strings.Trim(keyword, "# ")
		}
		v, err := b.service.SearchFeeds(ctx, keyword, xiaohongshu.FilterOption{SortBy: "最新", PublishTime: t.PublishTime, NoteType: t.NoteType})
		if err != nil {
			return nil, err
		}
		feeds = v.Feeds
	case "profile":
		user, token, err := parseConsoleURL(src.Value, src.Kind)
		if err != nil {
			return nil, err
		}
		v, err := b.service.UserProfile(ctx, user, token, "note")
		if err != nil {
			return nil, err
		}
		feeds = v.Feeds
	case "link":
		feed, token, err := parseConsoleURL(src.Value, src.Kind)
		if err != nil {
			return nil, err
		}
		return []cc.Post{{FeedID: feed, Token: token, URL: src.Value}}, nil
	default:
		return nil, fmt.Errorf("未知来源")
	}
	out := make([]cc.Post, 0, len(feeds))
	for _, v := range feeds {
		author := v.NoteCard.User.Nickname
		if author == "" {
			author = v.NoteCard.User.NickName
		}
		out = append(out, cc.Post{FeedID: v.ID, Token: v.XsecToken, Title: v.NoteCard.DisplayTitle, Author: author, URL: "https://www.xiaohongshu.com/explore/" + url.PathEscape(v.ID) + "?xsec_token=" + url.QueryEscape(v.XsecToken) + "&xsec_source=pc_search"})
	}
	return out, nil
}
