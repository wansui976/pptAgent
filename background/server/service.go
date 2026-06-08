package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"gorm.io/gorm"

	"lingxi/background/agent"
	"lingxi/background/shared"
	"lingxi/background/shared/log"
	"lingxi/background/tool"
	"lingxi/background/vo"
)

const DefaultWorkspaceRoot = "/workspace/lingxi/workspace"
const DefaultFrontRoot = "/workspace/lingxi/front"
const DefaultPPTistDistRoot = "/workspace/lingxi/PPTist/dist"
const DefaultPPTMasterRoot = "/workspace/ppt-master"

type ServerPaths struct {
	WorkspaceRoot string
	FrontRoot     string
	PPTistRoot    string
	PPTMasterRoot string
}

// ResolvePaths 从 config 读取路径，未配置时回落到默认值（向后兼容）。
func ResolvePaths(cfg shared.PathsConfig) ServerPaths {
	p := ServerPaths{
		WorkspaceRoot: DefaultWorkspaceRoot,
		FrontRoot:     DefaultFrontRoot,
		PPTistRoot:    DefaultPPTistDistRoot,
		PPTMasterRoot: DefaultPPTMasterRoot,
	}
	if cfg.WorkspaceRoot != "" {
		p.WorkspaceRoot = cfg.WorkspaceRoot
	}
	if cfg.FrontRoot != "" {
		p.FrontRoot = cfg.FrontRoot
	}
	if cfg.PPTistRoot != "" {
		p.PPTistRoot = cfg.PPTistRoot
	}
	if cfg.PPTMasterRoot != "" {
		p.PPTMasterRoot = cfg.PPTMasterRoot
	}
	return p
}

type Server struct {
	db            *gorm.DB
	agent         *agent.Agent
	workspaceRoot string
	frontRoot     string
	pptistRoot    string
	pptMasterRoot string
}

func NewServer(db *gorm.DB, agent *agent.Agent, paths ServerPaths) *Server {
	return &Server{
		db:            db,
		agent:         agent,
		workspaceRoot: paths.WorkspaceRoot,
		frontRoot:     paths.FrontRoot,
		pptistRoot:    paths.PPTistRoot,
		pptMasterRoot: paths.PPTMasterRoot,
	}
}

func (s *Server) UserWorkspaceRoot(userID string) string {
	return filepath.Join(s.workspaceRoot, "users", safePathSegment(userID))
}

func safePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	cleaned := strings.Trim(b.String(), "_")
	if cleaned == "" {
		return "unknown"
	}
	if len(cleaned) > 80 {
		return cleaned[:80]
	}
	return cleaned
}

func (s *Server) runtimeAgentForUser(userID string) (*agent.Agent, string, error) {
	if s.agent == nil {
		return nil, "", fmt.Errorf("agent is not configured")
	}
	workspaceRoot := s.UserWorkspaceRoot(userID)
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		return nil, "", err
	}
	tools := []tool.Tool{
		tool.CreateBashToolWithPPTMaster(workspaceRoot, s.pptMasterRoot),
		tool.NewReadToolWithRoots(workspaceRoot, s.pptMasterRoot),
		tool.NewWriteToolWithRoot(workspaceRoot),
		tool.NewEditToolWithRoot(workspaceRoot),
		tool.NewLoadSkillToolWithRoot(s.pptMasterRoot),
		tool.NewListPPTTemplatesToolWithRoot(s.pptMasterRoot),
	}
	return s.agent.CloneForWorkspace(workspaceRoot, tools), workspaceRoot, nil
}

func (s *Server) CreateConversation(req vo.CreateConversationReq) (vo.ConversationVO, error) {
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return vo.ConversationVO{}, fmt.Errorf("user_id is required")
	}
	conv := Conversation{
		ConversationID: uuid.New().String(),
		UserID:         userID,
		Title:          req.Title,
		CreatedAt:      time.Now().Unix(),
	}
	if err := s.db.Create(&conv).Error; err != nil {
		return vo.ConversationVO{}, err
	}
	return vo.ConversationVO{
		ConversationID: conv.ConversationID,
		UserID:         conv.UserID,
		Title:          conv.Title,
		CreatedAt:      conv.CreatedAt,
	}, nil
}

func (s *Server) ListConversations(userID string) ([]vo.ConversationVO, error) {
	var convs []Conversation
	query := s.db.Order("created_at desc")
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.Find(&convs).Error; err != nil {
		return nil, err
	}

	result := make([]vo.ConversationVO, 0, len(convs))
	for _, conv := range convs {
		result = append(result, vo.ConversationVO{
			ConversationID: conv.ConversationID,
			UserID:         conv.UserID,
			Title:          conv.Title,
			CreatedAt:      conv.CreatedAt,
		})
	}
	return result, nil
}

func (s *Server) RenameConversation(conversationID string, title string) (vo.ConversationVO, error) {
	if err := s.db.Model(&Conversation{}).
		Where("conversation_id = ?", conversationID).
		Update("title", title).Error; err != nil {
		return vo.ConversationVO{}, err
	}

	var conv Conversation
	if err := s.db.First(&conv, "conversation_id = ?", conversationID).Error; err != nil {
		return vo.ConversationVO{}, err
	}

	return vo.ConversationVO{
		ConversationID: conv.ConversationID,
		UserID:         conv.UserID,
		Title:          conv.Title,
		CreatedAt:      conv.CreatedAt,
	}, nil
}

func (s *Server) RenameConversationForUser(userID string, conversationID string, title string) (vo.ConversationVO, error) {
	if strings.TrimSpace(userID) == "" {
		return vo.ConversationVO{}, fmt.Errorf("user_id is required")
	}
	result := s.db.Model(&Conversation{}).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Update("title", title)
	if result.Error != nil {
		return vo.ConversationVO{}, result.Error
	}
	if result.RowsAffected == 0 {
		return vo.ConversationVO{}, gorm.ErrRecordNotFound
	}

	var conv Conversation
	if err := s.db.First(&conv, "conversation_id = ? AND user_id = ?", conversationID, userID).Error; err != nil {
		return vo.ConversationVO{}, err
	}

	return vo.ConversationVO{
		ConversationID: conv.ConversationID,
		UserID:         conv.UserID,
		Title:          conv.Title,
		CreatedAt:      conv.CreatedAt,
	}, nil
}

func (s *Server) DeleteConversation(conversationID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("conversation_id = ?", conversationID).
			Delete(&ChatMessage{}).Error; err != nil {
			return err
		}

		return tx.Where("conversation_id = ?", conversationID).
			Delete(&Conversation{}).Error
	})
}

func (s *Server) DeleteConversationForUser(userID string, conversationID string) error {
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("user_id is required")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var conv Conversation
		if err := tx.First(&conv, "conversation_id = ? AND user_id = ?", conversationID, userID).Error; err != nil {
			return err
		}
		if err := tx.Where("conversation_id = ? AND user_id = ?", conversationID, userID).
			Delete(&ChatMessage{}).Error; err != nil {
			return err
		}
		return tx.Where("conversation_id = ? AND user_id = ?", conversationID, userID).
			Delete(&Conversation{}).Error
	})
}

func (s *Server) ListMessages(conversationID string) ([]vo.ChatMessageVO, error) {
	var msgs []ChatMessage
	if err := s.db.Where("conversation_id = ?", conversationID).
		Order("created_at asc").Find(&msgs).Error; err != nil {
		return nil, err
	}

	result := make([]vo.ChatMessageVO, 0, len(msgs))
	for _, msg := range msgs {
		result = append(result, vo.ChatMessageVO{
			MessageID:       msg.MessageID,
			ConversationID:  msg.ConversationID,
			ParentMessageID: msg.ParentMessageID,
			Query:           msg.Query,
			Response:        msg.Response,
			Model:           msg.Model,
			CreatedAt:       msg.CreatedAt,
			Rounds:          parseRounds(msg.Rounds),
		})
	}
	return result, nil
}

func (s *Server) ListMessagesForUser(userID string, conversationID string) ([]vo.ChatMessageVO, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	var conv Conversation
	if err := s.db.First(&conv, "conversation_id = ? AND user_id = ?", conversationID, userID).Error; err != nil {
		return nil, err
	}
	return s.ListMessages(conversationID)
}

// CreateMessage 验证会话、构建历史、保存消息记录，并启动 agent 流式执行。
func (s *Server) CreateMessage(ctx context.Context, conversationID string, req vo.CreateMessageReq, voCh chan<- vo.SSEMessageVO) error {
	// 验证会话存在
	var conv Conversation
	if err := s.db.Where("conversation_id = ?", conversationID).First(&conv).Error; err != nil {
		return err
	}
	if strings.TrimSpace(req.UserID) == "" || conv.UserID != req.UserID {
		return fmt.Errorf("conversation not found")
	}
	runAgent, workspaceRoot, err := s.runtimeAgentForUser(conv.UserID)
	if err != nil {
		return err
	}

	// 从历史消息构建 history
	var historyMsgs []ChatMessage
	if err := s.db.Where("conversation_id = ?", conversationID).
		Order("created_at asc").Find(&historyMsgs).Error; err != nil {
		return err
	}
	history := buildHistory(historyMsgs, req.ParentMessageID)

	// 读取父消息的 PPTStage 作为本轮初始 state。
	// 不能直接取 historyMsgs 末条——会话是树形结构，用户 retry/fork 时末条可能属于另一支线。
	var latestState agent.PPTPipelineState
	if req.ParentMessageID != "" {
		for i := range historyMsgs {
			if historyMsgs[i].MessageID == req.ParentMessageID {
				latestState = agent.UnmarshalState(historyMsgs[i].PPTStage)
				break
			}
		}
	}
	stageStart := s.decideStageForAgent(runAgent, latestState, req.Query)

	msgID := uuid.New().String()
	createdAt := time.Now().Unix()

	eventCh := make(chan agent.StreamEvent, 64)
	done := make(chan struct{})
	var appendedContent strings.Builder
	// observedExport 记录本轮 SSE 中是否触发过 PPT 导出事件。
	// goroutine 写入、主 goroutine 在 close(eventCh) + <-done 之后读取，由 channel 的 happens-before 保证可见性。
	var observedExport bool
	sendVO := func(msg vo.SSEMessageVO) {
		select {
		case voCh <- msg:
		case <-ctx.Done():
		}
	}
	go func() {
		defer close(done)
		for e := range eventCh {
			sendVO(toSSEMessage(msgID, e))
			for _, extra := range pptInterceptorWithBase(e, workspaceRoot) {
				sendVO(toSSEMessage(msgID, extra))
				if extra.Event == agent.EventPPTExported {
					observedExport = true
					content := buildPPTDownloadContent(extra)
					if content != "" {
						appendedContent.WriteString(content)
						sendVO(toSSEMessage(msgID, agent.StreamEvent{
							Event:   agent.EventContent,
							Content: content,
						}))
					}
				}
			}
		}
	}()

	var (
		result     agent.RunResult
		runErr     error
		finalState agent.PPTPipelineState = stageStart
	)
	if agent.IsActiveStage(stageStart.Stage) && runAgent.PipelineV2Enabled() {
		stageOut, err := runAgent.RunStage(ctx, history, req.Query, stageStart, eventCh)
		runErr = err
		result = stageOut.Run
		finalState = stageOut.NextState
		if stageOut.FellBackToLegacy {
			sendVO(toSSEMessage(msgID, agent.StreamEvent{
				Event:   agent.EventContent,
				Content: "\n[流水线已切换至兼容模式：阶段输出连续 2 次校验失败]\n",
			}))
		}
	} else {
		// 进入旧 RunStreaming 时，如果 state 里已积累了 outline/layout（render 阶段或 legacy 兜底），
		// 把它们前置注入到 query，让旧 SKILL.md 流程能消费上一阶段的产出，避免新流水线白干。
		// 同时给前端发一个 stage 事件，让进度条能从 layout 跳到 render（否则 SSE 里没人发 stage）。
		if stageStart.Stage == agent.StageRender || stageStart.Stage == agent.StageExport {
			sendVO(toSSEMessage(msgID, agent.StreamEvent{Event: agent.EventContent, Stage: string(stageStart.Stage), Content: ""}))
		}
		augmentedQuery := injectPipelineContext(req.Query, stageStart)
		result, runErr = runAgent.RunStreaming(ctx, history, augmentedQuery, eventCh)
	}
	close(eventCh)
	<-done
	if runErr != nil {
		log.Warnf("run streaming error: %v", runErr)
	}
	if appended := appendedContent.String(); appended != "" {
		result.Response += appended
		result.Rounds = append(result.Rounds, openai.ChatCompletionMessage{Content: appended}.ToParam())
	}
	// 若本轮观察到 PPT 导出事件，把 stage 推到 StageExport，让前端进度条能走到最后一格。
	// 仅在已经处于 render/export 链路时推进——避免误把普通对话标成导出。
	if observedExport && (finalState.Stage == agent.StageRender || finalState.Stage == agent.StageExport) {
		finalState.Stage = agent.StageExport
		sendVO(toSSEMessage(msgID, agent.StreamEvent{Event: agent.EventContent, Stage: string(agent.StageExport), Content: ""}))
	}

	// agent 出错且没有任何可保存的内容时跳过落库，避免脏记录污染历史。
	if runErr != nil && result.Response == "" && len(result.Rounds) == 0 {
		return runErr
	}

	roundsJSON, _ := json.Marshal(result.Rounds)
	usageJSON, _ := json.Marshal(result.Usage)
	s.db.Create(&ChatMessage{
		MessageID:       msgID,
		UserID:          req.UserID,
		ConversationID:  conversationID,
		ParentMessageID: req.ParentMessageID,
		Query:           req.Query,
		Response:        result.Response,
		Rounds:          string(roundsJSON),
		Usage:           string(usageJSON),
		Model:           runAgent.Model(),
		PPTStage:        agent.MarshalState(finalState),
		CreatedAt:       createdAt,
	})
	if len(historyMsgs) == 0 && isDefaultConversationTitle(conv.Title) {
		title := s.generateConversationTitle(ctx, req.Query)
		if title != "" {
			if err := s.db.Model(&Conversation{}).
				Where("conversation_id = ?", conversationID).
				Update("title", title).Error; err != nil {
				log.Warnf("update conversation title failed: %v", err)
			}
		}
	}

	return nil
}

func isDefaultConversationTitle(title string) bool {
	trimmed := strings.TrimSpace(title)
	return trimmed == "" || trimmed == "新会话" || strings.EqualFold(trimmed, "new chat")
}

func summarizeConversationTitle(query string) string {
	text := strings.TrimSpace(query)
	if text == "" {
		return "新会话"
	}
	text = stripConversationTitleNoise(text)
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 22 {
		text = string(runes[:22])
	}
	return strings.Trim(strings.TrimSpace(text), "，。,.；;：:、|/- ")
}

func (s *Server) generateConversationTitle(ctx context.Context, query string) string {
	if s.agent == nil {
		return summarizeConversationTitle(query)
	}
	prompt := "请根据用户第一条消息总结一个中文会话标题。要求：不超过12个汉字；不要标点；不要引号；只输出标题。\n\n用户消息：" + stripConversationTitleNoise(query)
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return summarizeConversationTitle(query)
	}
	titleCtx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	resp, err := s.agent.Client().Chat.Completions.New(titleCtx, openai.ChatCompletionNewParams{
		Model: s.agent.Model(),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("你是会话标题生成器，只输出短标题。"),
			openai.UserMessage(prompt),
		},
	})
	if err != nil || len(resp.Choices) == 0 {
		return summarizeConversationTitle(query)
	}
	title := sanitizeConversationTitle(resp.Choices[0].Message.Content)
	if title == "" {
		return summarizeConversationTitle(query)
	}
	return title
}

func sanitizeConversationTitle(title string) string {
	cleaned := strings.TrimSpace(title)
	cleaned = strings.Trim(cleaned, "`'\"“”‘’ ，。,.；;：:、|/-\n\t")
	cleaned = strings.Join(strings.Fields(cleaned), "")
	runes := []rune(cleaned)
	if len(runes) > 12 {
		cleaned = string(runes[:12])
	}
	return cleaned
}

func stripConversationTitleNoise(text string) string {
	patterns := []string{
		`(?s)\n\n这是一个 PPT 需求。.*$`,
		`(?s)\n\n请直接使用模板.*$`,
		`(?s)\n\n不要使用任何预设模板.*$`,
		`(?s)\n\n以下是本次上传附件的补充信息：.*$`,
		`(?s)请先调用\s*list_ppt_templates\s*工具.*$`,
		`(?s)此轮不要开始生成 PPT.*$`,
	}
	cleaned := text
	for _, pattern := range patterns {
		cleaned = regexp.MustCompile(pattern).ReplaceAllString(cleaned, "")
	}
	replacers := []struct{ old, new string }{
		{"请直接使用模板", ""},
		{"不要再次进入模板选择步骤", ""},
		{"生成一份", ""},
		{"帮我", ""},
		{"请", ""},
	}
	for _, replacer := range replacers {
		cleaned = strings.ReplaceAll(cleaned, replacer.old, replacer.new)
	}
	return cleaned
}

func buildPPTDownloadContent(e agent.StreamEvent) string {
	if e.PPTPPTXURL == "" {
		return ""
	}
	downloadURL := e.PPTPPTXURL
	if strings.HasPrefix(downloadURL, "/") {
		downloadURL = "http://localhost:8080" + downloadURL
	}
	fileName := e.PPTFileName
	if fileName == "" {
		fileName = "PPTX 文件"
	}
	return fmt.Sprintf("\n\nPPTX 已导出完成：[%s](%s)\n", fileName, downloadURL)
}

func preferEditablePPTX(files []string) string {
	if len(files) == 0 {
		return ""
	}

	var bestNonSVG string
	var bestNonSVGMod time.Time
	var bestAny string
	var bestAnyMod time.Time

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil || info.IsDir() {
			continue
		}
		modTime := info.ModTime()
		if bestAny == "" || modTime.After(bestAnyMod) {
			bestAny = file
			bestAnyMod = modTime
		}
		if strings.Contains(strings.ToLower(filepath.Base(file)), "_svg.") {
			continue
		}
		if bestNonSVG == "" || modTime.After(bestNonSVGMod) {
			bestNonSVG = file
			bestNonSVGMod = modTime
		}
	}

	if bestNonSVG != "" {
		return bestNonSVG
	}
	return bestAny
}

func (s *Server) OpenProjectExportInWPS(projectName, fileName string) error {
	return s.openProjectExportInWPSAtRoot(s.workspaceRoot, projectName, fileName)
}

func (s *Server) OpenProjectExportInWPSForUser(userID, projectName, fileName string) error {
	return s.openProjectExportInWPSAtRoot(s.UserWorkspaceRoot(userID), projectName, fileName)
}

func (s *Server) openProjectExportInWPSAtRoot(workspaceRoot, projectName, fileName string) error {
	projectName = filepath.Base(projectName)
	fileName = filepath.Base(fileName)
	if projectName == "." || projectName == "" {
		return fmt.Errorf("invalid project export path")
	}

	exportPath := ""
	if fileName != "" && fileName != "." {
		exportPath = filepath.Join(workspaceRoot, projectName, "exports", fileName)
	}
	if exportPath == "" || strings.Contains(strings.ToLower(filepath.Base(exportPath)), "_svg.") {
		files, err := filepath.Glob(filepath.Join(workspaceRoot, projectName, "exports", "*.pptx"))
		if err == nil {
			if preferred := preferEditablePPTX(files); preferred != "" {
				exportPath = preferred
			}
		}
	}
	info, err := os.Stat(exportPath)
	if err != nil || info.IsDir() {
		return fmt.Errorf("export file not found")
	}

	if override := os.Getenv("LINGXI_OPEN_PPT_COMMAND"); override != "" {
		cmd := exec.Command("sh", "-c", override+" "+shellQuote(exportPath))
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to open export file: %w", err)
		}
		return nil
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-a", "WPS Office", exportPath)
		if err := cmd.Run(); err == nil {
			return nil
		}
		cmd = exec.Command("open", exportPath)
	case "windows":
		cmd = exec.Command("cmd", "/C", "start", "", exportPath)
	default:
		cmd = exec.Command("xdg-open", exportPath)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to open export file: %w", err)
	}
	return nil
}

func (s *Server) pptTemplateLayoutsDir() string {
	root := s.pptMasterRoot
	if root == "" {
		root = DefaultPPTMasterRoot
	}
	return filepath.Join(root, "skills/ppt-master/templates/layouts")
}

func (s *Server) ListPPTTemplates() ([]vo.PPTTemplateVO, error) {
	indexPath := filepath.Join(s.pptTemplateLayoutsDir(), "layouts_index.json")
	content, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}

	var index struct {
		Categories map[string]struct {
			Label   string   `json:"label"`
			Layouts []string `json:"layouts"`
		} `json:"categories"`
		Layouts map[string]struct {
			Label     string   `json:"label"`
			Summary   string   `json:"summary"`
			Tone      string   `json:"tone"`
			ThemeMode string   `json:"themeMode"`
			Keywords  []string `json:"keywords"`
			Assets    []string `json:"assets"`
		} `json:"layouts"`
	}
	if err := json.Unmarshal(content, &index); err != nil {
		return nil, err
	}

	categoryByLayout := make(map[string][]string)
	for categoryKey, category := range index.Categories {
		label := category.Label
		if label == "" {
			label = categoryKey
		}
		for _, layout := range category.Layouts {
			categoryByLayout[layout] = append(categoryByLayout[layout], label)
		}
	}

	names := make([]string, 0, len(index.Layouts))
	for name := range index.Layouts {
		names = append(names, name)
	}
	sort.Strings(names)

	templates := make([]vo.PPTTemplateVO, 0, len(names))
	for _, name := range names {
		layout := index.Layouts[name]
		categories := append([]string(nil), categoryByLayout[name]...)
		sort.Strings(categories)

		assetURLs := make([]string, 0, len(layout.Assets))
		for _, asset := range layout.Assets {
			assetURLs = append(assetURLs, fmt.Sprintf("/api/ppt/templates/%s/assets/%s", name, asset))
		}

		previews := s.existingTemplatePreviews(name)
		templates = append(templates, vo.PPTTemplateVO{
			Name:       name,
			Label:      layout.Label,
			Categories: categories,
			Summary:    layout.Summary,
			Tone:       layout.Tone,
			ThemeMode:  layout.ThemeMode,
			Keywords:   layout.Keywords,
			PreviewSVGURL: func() string {
				if len(previews) > 0 {
					return previews[0]
				}
				return ""
			}(),
			PreviewSVGURLs: previews,
			AssetURLs:      assetURLs,
		})
	}

	return templates, nil
}

func (s *Server) existingTemplatePreviews(templateName string) []string {
	candidates := []string{"01_cover.svg", "02_toc.svg", "02_chapter.svg", "03_content.svg", "04_ending.svg"}
	templateDir := filepath.Join(s.pptTemplateLayoutsDir(), templateName)
	previews := make([]string, 0, len(candidates))
	for _, name := range candidates {
		fullPath := filepath.Join(templateDir, name)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			previews = append(previews, fmt.Sprintf("/api/ppt/templates/%s/assets/%s", templateName, name))
		}
	}
	return previews
}

func toSSEMessage(msgID string, e agent.StreamEvent) vo.SSEMessageVO {
	msg := vo.SSEMessageVO{MessageID: msgID, Event: e.Event, Stage: e.Stage}
	switch e.Event {
	case agent.EventReasoning:
		msg.ReasoningContent = &e.ReasoningContent
	case agent.EventContent, agent.EventError:
		msg.Content = &e.Content
	case agent.EventToolCall:
		msg.ToolCallID = &e.ToolCallID
		msg.ToolCall = &e.ToolCall
		msg.ToolArguments = &e.ToolArguments
	case agent.EventToolResult:
		msg.ToolCallID = &e.ToolCallID
		msg.ToolCall = &e.ToolCall
		msg.ToolArguments = &e.ToolArguments
		msg.ToolResult = &e.ToolResult
	case agent.EventPPTProjectCreated:
		msg.PPTProjectName = &e.PPTProjectName
		msg.PPTProjectPath = &e.PPTProjectPath
	case agent.EventPPTPageSVG:
		msg.PPTPageIndex = &e.PPTPageIndex
		msg.PPTFileName = &e.PPTFileName
		msg.PPTSVGContent = &e.PPTSVGContent
	case agent.EventPPTExported:
		msg.PPTFileName = &e.PPTFileName
		msg.PPTPPTXURL = &e.PPTPPTXURL
	}
	return msg
}

func pptInterceptor(e agent.StreamEvent) []agent.StreamEvent {
	return pptInterceptorWithBase(e, DefaultWorkspaceRoot)
}

func pptInterceptorWithBase(e agent.StreamEvent, workspaceRoot string) []agent.StreamEvent {
	if e.Event != agent.EventToolResult {
		return nil
	}

	switch e.ToolCall {
	case "write":
		return interceptWriteResult(e)
	case "bash":
		return interceptBashResult(e, workspaceRoot)
	default:
		return nil
	}
}

func interceptWriteResult(e agent.StreamEvent) []agent.StreamEvent {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(e.ToolArguments), &args); err != nil {
		return nil
	}

	matched, _ := regexp.MatchString(`/svg_output/(?:\d+_|slide_?\d+[_-]).+\.svg$`, filepath.ToSlash(args.Path))
	if !matched {
		return nil
	}

	content, err := os.ReadFile(args.Path)
	if err != nil {
		return nil
	}

	pageIndex := extractPageIndex(filepath.Base(args.Path))
	fileName := filepath.Base(args.Path)
	return []agent.StreamEvent{{
		Event:         agent.EventPPTPageSVG,
		PPTPageIndex:  pageIndex,
		PPTFileName:   fileName,
		PPTSVGContent: string(content),
	}}
}

func interceptBashResult(e agent.StreamEvent, workspaceRoot string) []agent.StreamEvent {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(e.ToolArguments), &args); err != nil {
		return nil
	}

	events := make([]agent.StreamEvent, 0, 2)
	command := args.Command

	// 优先识别明确的命令，避免对同一 projectPath 重复广播 PPTPageSVG。
	switch {
	case strings.Contains(command, "project_manager.py init"):
		if projectPath := extractCreatedProjectPath(e.ToolResult, workspaceRoot); projectPath != "" {
			events = append(events, agent.StreamEvent{
				Event:          agent.EventPPTProjectCreated,
				PPTProjectName: filepath.Base(projectPath),
				PPTProjectPath: projectPath,
			})
			events = append(events, buildExistingPPTPageEvents(projectPath)...)
		}
	case strings.Contains(command, "svg_to_pptx.py"):
		if projectPath := extractProjectRootWithBase(command, e.ToolResult, workspaceRoot); projectPath != "" {
			events = append(events, buildExistingPPTPageEvents(projectPath)...)
			if event, ok := buildExportEvent(projectPath); ok {
				events = append(events, event)
			}
		}
	default:
		if projectPath := extractProjectRootWithBase(command, e.ToolResult, workspaceRoot); projectPath != "" {
			events = append(events, buildExistingPPTPageEvents(projectPath)...)
		}
	}

	return events
}

func buildExistingPPTPageEvents(projectPath string) []agent.StreamEvent {
	files, err := filepath.Glob(filepath.Join(projectPath, "svg_output", "*.svg"))
	if err != nil || len(files) == 0 {
		return nil
	}

	events := make([]agent.StreamEvent, 0, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		events = append(events, agent.StreamEvent{
			Event:         agent.EventPPTPageSVG,
			PPTPageIndex:  extractPageIndex(filepath.Base(file)),
			PPTFileName:   filepath.Base(file),
			PPTSVGContent: string(content),
		})
	}
	return events
}

func extractPageIndex(fileName string) int {
	if match := regexp.MustCompile(`^(?:slide_?)?(\d+)`).FindStringSubmatch(fileName); len(match) > 1 {
		pageIndex, _ := strconv.Atoi(match[1])
		return pageIndex
	}
	prefix := strings.SplitN(fileName, "_", 2)[0]
	pageIndex, _ := strconv.Atoi(prefix)
	return pageIndex
}

func extractProjectPath(command string, output string, workspaceRoot string) string {
	if path := extractCreatedProjectPath(output, workspaceRoot); path != "" {
		return path
	}
	root := regexp.QuoteMeta(filepath.ToSlash(workspaceRoot))
	re := regexp.MustCompile(root + `/[^\s]+`)
	if match := re.FindString(command); match != "" {
		return match
	}
	nameRe := regexp.MustCompile(`project_manager\.py\s+init\s+([^\s]+)`)
	if match := nameRe.FindStringSubmatch(command); len(match) > 1 {
		candidates, _ := filepath.Glob(filepath.Join(workspaceRoot, match[1]+"*"))
		var latest string
		var latestMod time.Time
		for _, candidate := range candidates {
			info, err := os.Stat(candidate)
			if err != nil || !info.IsDir() {
				continue
			}
			if latest == "" || info.ModTime().After(latestMod) {
				latest = candidate
				latestMod = info.ModTime()
			}
		}
		if latest != "" {
			return latest
		}
	}
	return ""
}

func extractCreatedProjectPath(output string, workspaceRoot string) string {
	root := regexp.QuoteMeta(filepath.ToSlash(workspaceRoot))
	re := regexp.MustCompile(`Project created:\s*(` + root + `/[^\s]+)`)
	if match := re.FindStringSubmatch(output); len(match) > 1 {
		return match[1]
	}
	re = regexp.MustCompile(`Project created at\s*(` + root + `/[^\s]+)`)
	if match := re.FindStringSubmatch(output); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractProjectRootWithBase(command string, output string, workspaceRoot string) string {
	root := regexp.QuoteMeta(filepath.ToSlash(workspaceRoot))
	re := regexp.MustCompile(`(` + root + `/[^\s]+)/(?:exports|svg_output|svg_final|notes)`)
	if match := re.FindStringSubmatch(output); len(match) > 1 {
		return match[1]
	}
	if match := re.FindStringSubmatch(command); len(match) > 1 {
		return match[1]
	}
	if path := extractCreatedProjectPath(output, workspaceRoot); path != "" {
		return path
	}
	if path := extractProjectPath(command, output, workspaceRoot); path != "" {
		return path
	}
	return ""
}

func buildExportEvent(projectPath string) (agent.StreamEvent, bool) {
	files, err := filepath.Glob(filepath.Join(projectPath, "exports", "*.pptx"))
	if err != nil || len(files) == 0 {
		return agent.StreamEvent{}, false
	}

	latest := preferEditablePPTX(files)
	if latest == "" {
		return agent.StreamEvent{}, false
	}

	projectName := filepath.Base(projectPath)
	fileName := filepath.Base(latest)
	url := fmt.Sprintf("/api/projects/%s/exports/%s", projectName, fileName)
	return agent.StreamEvent{
		Event:       agent.EventPPTExported,
		PPTFileName: fileName,
		PPTPPTXURL:  url,
	}, true
}

func defaultPPTistTheme() map[string]any {
	return map[string]any{
		"themeColors":     []string{"#5b9bd5", "#ed7d31", "#a5a5a5", "#ffc000", "#4472c4", "#70ad47"},
		"fontColor":       "#333",
		"fontName":        "",
		"backgroundColor": "#fff",
		"shadow": map[string]any{
			"h":     3,
			"v":     3,
			"blur":  2,
			"color": "#808080",
		},
		"outline": map[string]any{
			"width": 2,
			"color": "#525252",
			"style": "solid",
		},
	}
}

func buildPPTistPlaceholderSlide() map[string]any {
	return map[string]any{
		"id":       "lingxi-placeholder",
		"type":     "content",
		"elements": []map[string]any{},
		"background": map[string]any{
			"type":  "solid",
			"color": "#ffffff",
		},
	}
}

func inferPPTistSlideType(fileName string, pageIndex int) string {
	name := strings.ToLower(fileName)
	switch {
	case strings.Contains(name, "cover"):
		return "cover"
	case strings.Contains(name, "toc"), strings.Contains(name, "contents"):
		return "contents"
	case strings.Contains(name, "ending"), strings.Contains(name, "end"), strings.Contains(name, "thanks"):
		return "end"
	case pageIndex == 1:
		return "cover"
	default:
		return "content"
	}
}

func (s *Server) buildGeneratedPPTistSlides(projectName string) ([]map[string]any, error) {
	return s.buildGeneratedPPTistSlidesAtRoot(s.workspaceRoot, projectName)
}

func (s *Server) buildGeneratedPPTistSlidesAtRoot(workspaceRoot, projectName string) ([]map[string]any, error) {
	projectRoot := filepath.Join(workspaceRoot, projectName)
	svgDir := "svg_final"
	files, err := filepath.Glob(filepath.Join(projectRoot, svgDir, "*.svg"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		svgDir = "svg_output"
		files, err = filepath.Glob(filepath.Join(projectRoot, svgDir, "*.svg"))
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool {
		left := extractPageIndex(filepath.Base(files[i]))
		right := extractPageIndex(filepath.Base(files[j]))
		if left == right {
			return filepath.Base(files[i]) < filepath.Base(files[j])
		}
		return left < right
	})

	slides := make([]map[string]any, 0, len(files))
	for _, file := range files {
		base := filepath.Base(file)
		pageIndex := extractPageIndex(base)
		slideURL := fmt.Sprintf("/api/projects/%s/assets/%s/%s", projectName, svgDir, base)
		elements, background, err := convertSVGFileToPPTistElements(file)
		if err != nil {
			elements = []map[string]any{}
			background = map[string]any{
				"type": "image",
				"image": map[string]any{
					"src":  slideURL,
					"size": "cover",
				},
			}
		}
		slides = append(slides, map[string]any{
			"id":         fmt.Sprintf("lingxi-slide-%03d", pageIndex),
			"type":       inferPPTistSlideType(base, pageIndex),
			"elements":   elements,
			"background": background,
		})
	}
	return slides, nil
}

func mergePPTistSlides(draftSlides []any, generatedSlides []map[string]any) []any {
	if len(draftSlides) == 0 {
		result := make([]any, 0, len(generatedSlides))
		for _, slide := range generatedSlides {
			result = append(result, slide)
		}
		return result
	}

	result := make([]any, 0, max(len(draftSlides), len(generatedSlides)))
	for index, draftSlide := range draftSlides {
		if index < len(generatedSlides) && shouldUseGeneratedSlide(draftSlide, generatedSlides[index]) {
			result = append(result, generatedSlides[index])
			continue
		}
		result = append(result, draftSlide)
	}
	if len(generatedSlides) > len(draftSlides) {
		for _, slide := range generatedSlides[len(draftSlides):] {
			result = append(result, slide)
		}
	}
	return result
}

func shouldUseGeneratedSlide(draftSlide any, generatedSlide map[string]any) bool {
	draftMap, ok := draftSlide.(map[string]any)
	if !ok {
		return false
	}
	draftElements, _ := draftMap["elements"].([]any)
	generatedElements, _ := generatedSlide["elements"].([]map[string]any)
	if len(draftElements) > 0 || len(generatedElements) == 0 {
		return false
	}
	background, _ := draftMap["background"].(map[string]any)
	backgroundType, _ := background["type"].(string)
	backgroundColor, _ := background["color"].(string)
	return backgroundType == "solid" && (backgroundColor == "" || strings.EqualFold(backgroundColor, "#ffffff") || strings.EqualFold(backgroundColor, "#fff"))
}

func projectPreferredExport(projectRoot string) (string, string) {
	files, err := filepath.Glob(filepath.Join(projectRoot, "exports", "*.pptx"))
	if err != nil || len(files) == 0 {
		return "", ""
	}
	best := preferEditablePPTX(files)
	if best == "" {
		return "", ""
	}
	return best, filepath.Base(best)
}

func (s *Server) GetPPTistProject(projectName string) (map[string]any, error) {
	return s.getPPTistProjectAtRoot(s.workspaceRoot, projectName)
}

func (s *Server) GetPPTistProjectForUser(userID string, projectName string) (map[string]any, error) {
	return s.getPPTistProjectAtRoot(s.UserWorkspaceRoot(userID), projectName)
}

func (s *Server) getPPTistProjectAtRoot(workspaceRoot string, projectName string) (map[string]any, error) {
	projectName = filepath.Base(projectName)
	if projectName == "." || projectName == "" {
		return nil, fmt.Errorf("invalid project name")
	}

	projectRoot := filepath.Join(workspaceRoot, projectName)
	info, err := os.Stat(projectRoot)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("project not found")
	}

	generatedSlides, err := s.buildGeneratedPPTistSlidesAtRoot(workspaceRoot, projectName)
	if err != nil {
		return nil, err
	}
	if len(generatedSlides) == 0 {
		generatedSlides = []map[string]any{buildPPTistPlaceholderSlide()}
	}

	payload := map[string]any{
		"title":          projectName,
		"width":          1000,
		"height":         562.5,
		"theme":          defaultPPTistTheme(),
		"slides":         mergePPTistSlides(nil, generatedSlides),
		"project_name":   projectName,
		"page_count":     len(generatedSlides),
		"source":         "lingxi",
		"draft_enabled":  true,
		"generated_only": true,
	}

	if exportPath, fileName := projectPreferredExport(projectRoot); exportPath != "" {
		payload["export_url"] = fmt.Sprintf("/api/projects/%s/exports/%s", projectName, fileName)
		payload["file_name"] = fileName
	}

	draftPath := filepath.Join(projectRoot, "pptist.json")
	if content, err := os.ReadFile(draftPath); err == nil {
		var draft map[string]any
		if json.Unmarshal(content, &draft) == nil {
			if title, ok := draft["title"].(string); ok && strings.TrimSpace(title) != "" {
				payload["title"] = title
			}
			if width, ok := draft["width"].(float64); ok && width > 0 {
				payload["width"] = width
			}
			if height, ok := draft["height"].(float64); ok && height > 0 {
				payload["height"] = height
			}
			if theme, ok := draft["theme"]; ok && theme != nil {
				payload["theme"] = theme
			}
			if slides, ok := draft["slides"].([]any); ok {
				payload["slides"] = mergePPTistSlides(slides, generatedSlides)
				payload["generated_only"] = false
			}
			if savedAt, ok := draft["saved_at"]; ok {
				payload["saved_at"] = savedAt
			}
		}
	}

	return payload, nil
}

func (s *Server) SavePPTistDraft(projectName string, body []byte) error {
	return s.savePPTistDraftAtRoot(s.workspaceRoot, projectName, body)
}

func (s *Server) SavePPTistDraftForUser(userID string, projectName string, body []byte) error {
	return s.savePPTistDraftAtRoot(s.UserWorkspaceRoot(userID), projectName, body)
}

func (s *Server) savePPTistDraftAtRoot(workspaceRoot string, projectName string, body []byte) error {
	projectName = filepath.Base(projectName)
	if projectName == "." || projectName == "" {
		return fmt.Errorf("invalid project name")
	}

	projectRoot := filepath.Join(workspaceRoot, projectName)
	info, err := os.Stat(projectRoot)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("project not found")
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("invalid pptist payload: %w", err)
	}
	stripAuthTokenFromProjectURLs(payload)
	payload["project_name"] = projectName
	payload["saved_at"] = time.Now().Format(time.RFC3339)

	formatted, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(projectRoot, "pptist.json"), formatted, 0o644)
}

func attachAuthTokenToProjectURLs(value any, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if text, ok := item.(string); ok {
				v[key] = withAuthTokenForProjectURL(text, token)
				continue
			}
			attachAuthTokenToProjectURLs(item, token)
		}
	case []any:
		for _, item := range v {
			attachAuthTokenToProjectURLs(item, token)
		}
	case []map[string]any:
		for _, item := range v {
			attachAuthTokenToProjectURLs(item, token)
		}
	}
}

func stripAuthTokenFromProjectURLs(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if text, ok := item.(string); ok {
				v[key] = withoutAuthToken(text)
				continue
			}
			stripAuthTokenFromProjectURLs(item)
		}
	case []any:
		for _, item := range v {
			stripAuthTokenFromProjectURLs(item)
		}
	case []map[string]any:
		for _, item := range v {
			stripAuthTokenFromProjectURLs(item)
		}
	}
}

func withAuthTokenForProjectURL(raw string, token string) string {
	if !strings.HasPrefix(raw, "/api/projects/") {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	query.Set("auth_token", token)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func withoutAuthToken(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	if _, ok := query["auth_token"]; !ok {
		return raw
	}
	query.Del("auth_token")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// parseRounds 将存储的 rounds JSON 转换为前端友好的 RoundMessageVO 列表。
func parseRounds(roundsJSON string) []vo.RoundMessageVO {
	if roundsJSON == "" {
		return nil
	}
	var msgs []shared.OpenAIMessage
	if err := json.Unmarshal([]byte(roundsJSON), &msgs); err != nil {
		return nil
	}

	result := make([]vo.RoundMessageVO, 0, len(msgs))
	toolNamesByID := make(map[string]string)
	for _, m := range msgs {
		switch {
		case m.OfUser != nil:
			// user 消息不需要展示
			continue

		case m.OfAssistant != nil:
			a := m.OfAssistant
			rv := vo.RoundMessageVO{Role: "assistant"}
			if len(a.ToolCalls) > 0 {
				for _, tc := range a.ToolCalls {
					if tc.OfFunction != nil {
						toolNamesByID[tc.OfFunction.ID] = tc.OfFunction.Function.Name
						rv.ToolCalls = append(rv.ToolCalls, vo.ToolCallVO{
							ID:        tc.OfFunction.ID,
							Name:      tc.OfFunction.Function.Name,
							Arguments: tc.OfFunction.Function.Arguments,
						})
					}
				}
				result = append(result, rv)
			}

		case m.OfTool != nil:
			t := m.OfTool
			result = append(result, vo.RoundMessageVO{
				Role:     "tool",
				ToolName: toolNamesByID[t.ToolCallID],
				ToolID:   t.ToolCallID,
				Content:  t.Content.OfString.Value,
			})
		}
	}
	return result
}

// shellQuote wraps an arbitrary string as a single POSIX shell argument
// using single quotes. Embedded single quotes are escaped with the standard
// close + backslash-escape + reopen idiom.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// decideStage 决定本轮请求该走哪个 stage。
// fallback 切换在 RunStage 内部发生（FailureCount>=2 时由 ensureNextStage 设置 StageLegacy），
// 这里只做"基于上一轮 state + 本轮 query"的纯路由，不会自己触发回退。
//
// 决策树：
//   - v2 关闭 → StageNone（走旧 RunStreaming）
//   - 历史 state == StageLegacy → 沿用 legacy
//   - 历史 state == StageIntake 且本轮用户输入像页数选择 → 写入 PageRange，推进到 StageResearch
//   - 历史 state 是 active stage → 沿用（让 agent 在该 stage 继续）
//   - 历史 state == StageRender / StageExport → 沿用，由 RunStreaming 全工具流程接手
//   - 历史 state 空 / StageNone → 若 agent.IsPPTRequest，进 StageIntake；否则 StageNone
func (s *Server) decideStage(prev agent.PPTPipelineState, query string) agent.PPTPipelineState {
	return s.decideStageForAgent(s.agent, prev, query)
}

func (s *Server) decideStageForAgent(runAgent *agent.Agent, prev agent.PPTPipelineState, query string) agent.PPTPipelineState {
	if runAgent == nil || !runAgent.PipelineV2Enabled() {
		return agent.PPTPipelineState{Stage: agent.StageNone}
	}
	if prev.Stage == agent.StageLegacy {
		return prev
	}
	if prev.Stage == agent.StageIntake {
		if pick := detectPageRangePick(query); pick != "" {
			prev.PageRange = pick
			prev.Stage = agent.StageResearch
			prev.FailureCount = 0
			return prev
		}
		// 用户没明确选档，重新走 intake。
		return prev
	}
	if agent.IsActiveStage(prev.Stage) {
		return prev
	}
	if prev.Stage == agent.StageRender || prev.Stage == agent.StageExport {
		return prev
	}
	if agent.IsPPTRequest(query) && runAgent.HasTool("web_search") {
		return agent.PPTPipelineState{Stage: agent.StageIntake}
	}
	return agent.PPTPipelineState{Stage: agent.StageNone}
}

// injectPipelineContext 把 v2 流水线攒出的 outline/layout 拼进 query 头部，
// 让旧 RunStreaming + SKILL.md 流程能直接读到。仅在 state 含有相应字段时注入；
// 没有任何 v2 上下文时返回原始 query。
func injectPipelineContext(query string, state agent.PPTPipelineState) string {
	outline := strings.TrimSpace(state.OutlineJSON)
	layout := strings.TrimSpace(state.LayoutPlanJSON)
	if outline == "" && layout == "" {
		return query
	}
	var b strings.Builder
	b.WriteString("# 流水线上一阶段产出（请直接采用，不要重新设计大纲或版式）\n\n")
	if state.Topic != "" {
		b.WriteString("## 主题\n" + state.Topic + "\n\n")
	}
	if state.PageRange != "" {
		b.WriteString("## 页数档位\n" + state.PageRange + "\n\n")
	}
	if outline != "" {
		b.WriteString("## PPT 大纲（来自 outline 阶段）\n```json\n" + outline + "\n```\n\n")
	}
	if layout != "" {
		b.WriteString("## 版式规划（来自 layout 阶段，按 page_index 顺序选 layout_id）\n```json\n" + layout + "\n```\n\n")
	}
	b.WriteString("# 用户本轮指令\n" + query)
	return b.String()
}

// detectPageRangePick 解析用户对 intake 阶段页数档位的回复。
// 前端会把按钮点击转成形如 "我选标准 15-20 页" 的文本；同时兼容直接输入档位关键词。
func detectPageRangePick(query string) string {
	lower := strings.ToLower(strings.TrimSpace(query))
	switch {
	case strings.Contains(lower, "8-12"), strings.Contains(lower, "简版"), strings.Contains(lower, "compact"):
		return "8-12 页"
	case strings.Contains(lower, "15-20"), strings.Contains(lower, "标准"), strings.Contains(lower, "standard"):
		return "15-20 页"
	case strings.Contains(lower, "25-35"), strings.Contains(lower, "详版"), strings.Contains(lower, "detailed"):
		return "25-35 页"
	}
	return ""
}
