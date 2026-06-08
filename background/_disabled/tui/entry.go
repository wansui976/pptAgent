package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	reasonStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	toolStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	errorStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	policyStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("228")).Bold(true)
	confirmStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	confirmBoxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	confirmSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	confirmOptionStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)

// LogEntry 日志条目结构体
type LogEntry struct {
	Title   string         // 标题（如 "你:", "推理:" 等）
	Content string         // 正文内容
	Style   lipgloss.Style // 渲染样式
}

// NewLabel 创建轮次标签
func NewLabel(content string) LogEntry {
	return LogEntry{Title: "", Content: content, Style: labelStyle}
}

// NewContent 创建用户输入
func NewContent(content string) LogEntry {
	return LogEntry{Title: "你", Content: content, Style: contentStyle}
}

// NewAnswer 创建 AI 回答
func NewAnswer(content string) LogEntry {
	return LogEntry{Title: "回答", Content: content, Style: contentStyle}
}

// NewReasoning 创建推理内容
func NewReasoning(content string) LogEntry {
	return LogEntry{Title: "推理", Content: content, Style: reasonStyle}
}

// NewTool 创建工具调用
func NewTool(content string) LogEntry {
	return LogEntry{Title: "工具调用", Content: content, Style: toolStyle}
}

// NewError 创建错误信息
func NewError(content string) LogEntry {
	return LogEntry{Title: "错误", Content: content, Style: errorStyle}
}

// NewPolicyRunning 创建策略运行中状态
func NewPolicyRunning(name string) LogEntry {
	return LogEntry{Title: "上下文策略", Content: fmt.Sprintf("%s (运行中...)", name), Style: policyStyle}
}

// UpdatePolicyCompleted 更新策略 log entry 为完成状态
func (e *LogEntry) UpdatePolicyCompleted(success bool) {
	status := "已完成"
	if !success {
		status = "已失败"
	}
	// 移除 " (运行中...)" 后缀并替换为完成状态
	e.Content = strings.Replace(e.Content, " (运行中...)", "", 1)
	e.Content = fmt.Sprintf("%s (%s)", e.Content, status)
}

// NewMemoryRunning 创建记忆更新运行中状态
func NewMemoryRunning() LogEntry {
	return LogEntry{Title: "记忆更新", Content: "(运行中...)", Style: policyStyle}
}

// UpdateMemoryCompleted 更新记忆 log entry 为完成状态
func (e *LogEntry) UpdateMemoryCompleted(success bool) {
	status := "已完成"
	if !success {
		status = "已失败"
	}
	// 移除 "(运行中...)" 后缀并替换为完成状态
	e.Content = strings.Replace(e.Content, "(运行中...)", "", 1)
	e.Content = fmt.Sprintf("%s (%s)", e.Content, status)
}

// NewBorder 创建分隔线
func NewBorder() LogEntry {
	return LogEntry{Title: "", Content: strings.Repeat("─", 48), Style: borderStyle}
}

// NewNotice 创建通知信息
func NewNotice(content string) LogEntry {
	return LogEntry{Title: "提示", Content: content, Style: noticeStyle}
}

// NewToolConfirmation 创建工具确认请求
func NewToolConfirmation(toolName, arguments string) LogEntry {
	content := fmt.Sprintf("%s(%s)", toolName, arguments)
	return LogEntry{
		Title:   "工具确认",
		Content: content,
		Style:   confirmStyle,
	}
}

// AppendContent 追加内容
func (e *LogEntry) AppendContent(chunk string) {
	e.Content += chunk
}

func (e *LogEntry) Render() string {
	if e.Title == "" {
		return e.Style.Render(e.Content)
	}
	return e.Style.Render(e.Title + ": " + e.Content)
}
