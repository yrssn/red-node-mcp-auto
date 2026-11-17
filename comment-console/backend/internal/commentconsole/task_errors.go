package commentconsole

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
)

// 只保存阶段与错误类别，原始错误可能包含 URL、凭证或浏览器堆栈。
type taskDiagnosticError struct {
	stage    string
	category string
}

func (e *taskDiagnosticError) Error() string {
	action, hint := "处理任务", "请重试当前步骤并检查运行环境"
	switch e.stage {
	case "account":
		action, hint = "核实登录账号", "请检查登录页面和账号状态后重试"
	case "discover":
		action, hint = "搜索来源", "请重试当前来源，或检查搜索页面与网络状态"
	case "detail":
		action, hint = "读取帖子详情", "请检查原帖是否可访问后重试"
	case "ocr":
		action, hint = "识别图片文字", "请检查本地 OCR 服务后重试"
	case "analyze":
		action, hint = "分析帖子内容", "请检查模型连接与配置后重试"
	case "waiting_browser":
		action, hint = "等待浏览器操作", "请等待当前浏览器操作结束后重试"
	}
	switch e.category {
	case "timeout":
		return action + "超时；" + hint
	case "canceled":
		return action + "已取消，本轮已停止"
	case "closed":
		if e.stage == "ocr" || e.stage == "analyze" {
			return action + "时连接已关闭；" + hint
		}
		return action + "时浏览器或页面已关闭，请重新打开后再运行"
	default:
		return action + "发生异常；" + hint
	}
}

func (e *taskDiagnosticError) Unwrap() error {
	switch e.category {
	case "timeout":
		return context.DeadlineExceeded
	case "canceled":
		return context.Canceled
	case "closed":
		return net.ErrClosed
	default:
		return nil
	}
}

func safeTaskFailure(stage string, value any) *taskDiagnosticError {
	result := &taskDiagnosticError{stage: stage, category: "unknown"}
	err, ok := value.(error)
	if !ok {
		return result
	}
	switch {
	case errors.Is(err, context.Canceled):
		result.category = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		result.category = "timeout"
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, cdp.ErrSessionNotFound):
		result.category = "closed"
	default:
		var timeout net.Error
		var page *rod.PageNotFoundError
		if errors.As(err, &timeout) && timeout.Timeout() {
			result.category = "timeout"
		} else if errors.As(err, &page) {
			result.category = "closed"
		}
	}
	return result
}
