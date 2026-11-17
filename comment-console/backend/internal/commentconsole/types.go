package commentconsole

import "context"

const Version = "1.0.0"

type Model struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key,omitempty"`
	HasKey  bool   `json:"has_key"`
}

type Strategy struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   int    `json:"version"`
	Enabled   bool   `json:"enabled"`
	Mode      string `json:"mode"`
	Prompt    string `json:"prompt"`
	FixedText string `json:"fixed_text"`
	MaxLength int    `json:"max_length"`
	Forbidden string `json:"forbidden"`
}

type Material struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Content  string `json:"content"`
	Image    string `json:"image,omitempty"`
	Enabled  bool   `json:"enabled"`
}

type Source struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Task struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Sources        []Source `json:"sources"`
	ModelID        string   `json:"model_id"`
	StrategyID     string   `json:"strategy_id"`
	Goal           string   `json:"goal"`
	AnalysisMode   string   `json:"analysis_mode"`
	MatchKeywords  string   `json:"match_keywords"`
	DemandKeywords string   `json:"demand_keywords"`
	IncludeClosed  bool     `json:"include_closed"`
	Exclude        string   `json:"exclude"`
	PublishTime    string   `json:"publish_time"`
	NoteType       string   `json:"note_type"`
	Interval       int      `json:"interval"`
	Limit          int      `json:"limit"`
	SendLimit      int      `json:"send_limit"`
	SendCount      int      `json:"send_count"`
	AutoSend       bool     `json:"auto_send"`
	Scheduled      bool     `json:"scheduled"`
	Paused         bool     `json:"paused"`
	CollectPaused  bool     `json:"collect_paused"`
	AnalyzePaused  bool     `json:"analyze_paused"`
	SendPaused     bool     `json:"send_paused"`
	NextRun        int64    `json:"next_run"`
	LastRun        int64    `json:"last_run"`
	LastError      string   `json:"last_error"`
	AccountID      string   `json:"account_id"`
	Running        bool     `json:"running"`
	Queued         bool     `json:"queued"`
	Phase          string   `json:"phase"`
	Progress       string   `json:"progress"`
}

type Post struct {
	ID             string   `json:"id"`
	TaskID         string   `json:"task_id"`
	FeedID         string   `json:"feed_id"`
	Token          string   `json:"token"`
	Title          string   `json:"title"`
	Content        string   `json:"content"`
	Images         []string `json:"images,omitempty"`
	OCRText        string   `json:"ocr_text"`
	OCRStatus      string   `json:"ocr_status"`
	OCRError       string   `json:"ocr_error"`
	AuthorUpdates  string   `json:"author_updates"`
	AnalysisMethod string   `json:"analysis_method"`
	DecisionStatus string   `json:"decision_status"`
	DemandClosed   bool     `json:"demand_closed"`
	Author         string   `json:"author"`
	URL            string   `json:"url"`
	Source         string   `json:"source"`
	Match          *bool    `json:"match"`
	Reason         string   `json:"reason"`
	CreatedAt      int64    `json:"created_at"`
}

type Revision struct {
	At     int64  `json:"at"`
	Action string `json:"action"`
	Text   string `json:"text"`
}

type Comment struct {
	ID               string     `json:"id"`
	TaskID           string     `json:"task_id"`
	PostID           string     `json:"post_id"`
	AccountID        string     `json:"account_id"`
	Original         string     `json:"original"`
	Text             string     `json:"text"`
	SentText         string     `json:"sent_text"`
	Status           string     `json:"status"`
	QueueReason      string     `json:"queue_reason,omitempty"`
	QueueBlocked     bool       `json:"queue_blocked"`
	Error            string     `json:"error"`
	Strategy         Strategy   `json:"strategy"`
	StrategyVersions []Strategy `json:"strategy_versions"`
	History          []Revision `json:"history"`
	CreatedAt        int64      `json:"created_at"`
	UpdatedAt        int64      `json:"updated_at"`
	SentAt           int64      `json:"sent_at"`
}

type Log struct {
	At      int64  `json:"at"`
	TaskID  string `json:"task_id"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type Settings struct {
	SendPaused   bool `json:"send_paused"`
	SendInterval int  `json:"send_interval"`
}

type State struct {
	Models     []Model    `json:"models"`
	Strategies []Strategy `json:"strategies"`
	Materials  []Material `json:"materials"`
	Tasks      []Task     `json:"tasks"`
	Posts      []Post     `json:"posts"`
	Comments   []Comment  `json:"comments"`
	Logs       []Log      `json:"logs"`
	Settings   Settings   `json:"settings"`
	Demo       bool       `json:"demo"`
	Version    string     `json:"version"`
}

type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LoggedIn bool   `json:"logged_in"`
}

// Backend 隔离站点自动化，演示模式与真实模式使用同一业务流程。
type Backend interface {
	Account(context.Context) (Account, error)
	QRCode(context.Context) (any, error)
	Logout(context.Context) error
	Discover(context.Context, Source, Task) ([]Post, error)
	Detail(context.Context, Post) (Post, error)
	Send(context.Context, Post, string) error
}

type Decision struct {
	Match   bool   `json:"match"`
	Reason  string `json:"reason"`
	Comment string `json:"comment"`
	Status  string `json:"status,omitempty"`
}
