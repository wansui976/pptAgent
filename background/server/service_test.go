package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"lingxi/background/agent"
	"lingxi/background/shared"
	"lingxi/background/vo"
)

func TestRenameConversation_UpdatesTitle(t *testing.T) {
	s := newTestServer(t)

	created, err := s.CreateConversation(vo.CreateConversationReq{
		UserID: "user_001",
		Title:  "Old Title",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	updated, err := s.RenameConversation(created.ConversationID, "New Title")
	if err != nil {
		t.Fatalf("RenameConversation() error = %v", err)
	}

	if updated.Title != "New Title" {
		t.Fatalf("updated title = %q, want %q", updated.Title, "New Title")
	}

	var stored Conversation
	if err := s.db.First(&stored, "conversation_id = ?", created.ConversationID).Error; err != nil {
		t.Fatalf("load stored conversation: %v", err)
	}

	if stored.Title != "New Title" {
		t.Fatalf("stored title = %q, want %q", stored.Title, "New Title")
	}
}

func TestCreateMessage_UpdatesDefaultConversationTitle(t *testing.T) {
	s := newTestServer(t)
	s.agent = agent.NewAgent(shared.ModelConfig{Model: "test-model"}, "", s.workspaceRoot, nil)

	created, err := s.CreateConversation(vo.CreateConversationReq{
		UserID: "user_001",
		Title:  "新会话",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if err := s.db.Create(&ChatMessage{
		MessageID:      "msg-1",
		UserID:         "user_001",
		ConversationID: created.ConversationID,
		Query:          "生成 3 张ppt 主题为对cs2的市场调研\n\n这是一个 PPT 需求。请先调用 list_ppt_templates 工具查看可用模板。",
		Response:       "ok",
		Model:          "test-model",
		CreatedAt:      time.Now().Unix(),
	}).Error; err != nil {
		t.Fatalf("seed chat message: %v", err)
	}

	if _, err := s.RenameConversation(created.ConversationID, summarizeConversationTitle("生成 3 张ppt 主题为对cs2的市场调研\n\n这是一个 PPT 需求。请先调用 list_ppt_templates 工具查看可用模板。")); err != nil {
		t.Fatalf("RenameConversation() error = %v", err)
	}

	var stored Conversation
	if err := s.db.First(&stored, "conversation_id = ?", created.ConversationID).Error; err != nil {
		t.Fatalf("load stored conversation: %v", err)
	}
	if stored.Title != "生成 3 张ppt 主题为对cs2的市场调研" {
		t.Fatalf("title = %q", stored.Title)
	}
}

func TestDeleteConversation_RemovesConversationAndMessages(t *testing.T) {
	s := newTestServer(t)

	created, err := s.CreateConversation(vo.CreateConversationReq{
		UserID: "user_001",
		Title:  "Delete Me",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	if err := s.db.Create(&ChatMessage{
		MessageID:       "msg-1",
		UserID:          "user_001",
		ConversationID:  created.ConversationID,
		ParentMessageID: "",
		Query:           "hello",
		Response:        "world",
		Model:           "test-model",
		CreatedAt:       time.Now().Unix(),
	}).Error; err != nil {
		t.Fatalf("seed chat message: %v", err)
	}

	if err := s.DeleteConversation(created.ConversationID); err != nil {
		t.Fatalf("DeleteConversation() error = %v", err)
	}

	var conversationCount int64
	if err := s.db.Model(&Conversation{}).
		Where("conversation_id = ?", created.ConversationID).
		Count(&conversationCount).Error; err != nil {
		t.Fatalf("count conversations: %v", err)
	}
	if conversationCount != 0 {
		t.Fatalf("conversation count = %d, want 0", conversationCount)
	}

	var messageCount int64
	if err := s.db.Model(&ChatMessage{}).
		Where("conversation_id = ?", created.ConversationID).
		Count(&messageCount).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if messageCount != 0 {
		t.Fatalf("message count = %d, want 0", messageCount)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}

	return NewServer(db, nil, ResolvePaths(shared.PathsConfig{}))
}

func TestPPTInterceptor_WriteSVGEmitsPageEvent(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "demo_ppt169_20260423")
	svgPath := filepath.Join(projectDir, "svg_output", "01_cover.svg")
	if err := os.MkdirAll(filepath.Dir(svgPath), 0o755); err != nil {
		t.Fatalf("mkdir svg dir: %v", err)
	}
	const svg = `<svg viewBox="0 0 1280 720"></svg>`
	if err := os.WriteFile(svgPath, []byte(svg), 0o644); err != nil {
		t.Fatalf("write svg: %v", err)
	}

	args, _ := json.Marshal(map[string]string{"path": svgPath})
	events := pptInterceptor(agent.StreamEvent{
		Event:         agent.EventToolResult,
		ToolCall:      "write",
		ToolArguments: string(args),
	})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Event != agent.EventPPTPageSVG {
		t.Fatalf("event = %q, want %q", events[0].Event, agent.EventPPTPageSVG)
	}
	if events[0].PPTPageIndex != 1 {
		t.Fatalf("page index = %d, want 1", events[0].PPTPageIndex)
	}
	if events[0].PPTSVGContent != svg {
		t.Fatalf("svg content mismatch")
	}
}

func TestPPTInterceptor_ProjectInitEmitsCreatedEvent(t *testing.T) {
	projectRoot := filepath.Join(DefaultWorkspaceRoot, "demo_ppt169_20260423")
	args, _ := json.Marshal(map[string]string{
		"command": "python3 /workspace/ppt-master/skills/ppt-master/scripts/project_manager.py init demo --format ppt169",
	})

	events := pptInterceptor(agent.StreamEvent{
		Event:         agent.EventToolResult,
		ToolCall:      "bash",
		ToolArguments: string(args),
		ToolResult:    "Project created at " + projectRoot + "\n",
	})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Event != agent.EventPPTProjectCreated {
		t.Fatalf("event = %q, want %q", events[0].Event, agent.EventPPTProjectCreated)
	}
	if events[0].PPTProjectPath != projectRoot {
		t.Fatalf("project path = %q, want %q", events[0].PPTProjectPath, projectRoot)
	}
}

func TestPPTInterceptor_ExportEmitsPPTXEvent(t *testing.T) {
	workspaceRoot := t.TempDir()
	projectRoot := filepath.Join(workspaceRoot, "export-demo")
	exportDir := filepath.Join(projectRoot, "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("mkdir exports dir: %v", err)
	}

	oldFile := filepath.Join(exportDir, "old.pptx")
	newFile := filepath.Join(exportDir, "new.pptx")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old export: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(newFile, []byte("new"), 0o644); err != nil {
		t.Fatalf("write new export: %v", err)
	}

	args, _ := json.Marshal(map[string]string{
		"command": "python3 /workspace/ppt-master/skills/ppt-master/scripts/svg_to_pptx.py " + projectRoot,
	})
	events := pptInterceptorWithBase(agent.StreamEvent{
		Event:         agent.EventToolResult,
		ToolCall:      "bash",
		ToolArguments: string(args),
		ToolResult:    "exported\n",
	}, workspaceRoot)

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Event != agent.EventPPTExported {
		t.Fatalf("event = %q, want %q", events[0].Event, agent.EventPPTExported)
	}
	if events[0].PPTFileName != "new.pptx" {
		t.Fatalf("file name = %q, want %q", events[0].PPTFileName, "new.pptx")
	}
	if events[0].PPTPPTXURL != "/api/projects/export-demo/exports/new.pptx" {
		t.Fatalf("url = %q", events[0].PPTPPTXURL)
	}
}

func TestPPTInterceptor_ExportPrefersEditablePPTXOverSVGVariant(t *testing.T) {
	workspaceRoot := t.TempDir()
	projectRoot := filepath.Join(workspaceRoot, "export-demo")
	exportDir := filepath.Join(projectRoot, "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("mkdir exports dir: %v", err)
	}

	svgFile := filepath.Join(exportDir, "deck_svg.pptx")
	editableFile := filepath.Join(exportDir, "deck.pptx")
	if err := os.WriteFile(svgFile, []byte("svg"), 0o644); err != nil {
		t.Fatalf("write svg export: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(editableFile, []byte("editable"), 0o644); err != nil {
		t.Fatalf("write editable export: %v", err)
	}

	args, _ := json.Marshal(map[string]string{
		"command": "python3 /workspace/ppt-master/skills/ppt-master/scripts/svg_to_pptx.py " + projectRoot,
	})
	events := pptInterceptorWithBase(agent.StreamEvent{
		Event:         agent.EventToolResult,
		ToolCall:      "bash",
		ToolArguments: string(args),
		ToolResult:    "exported\n",
	}, workspaceRoot)

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].PPTFileName != "deck.pptx" {
		t.Fatalf("file name = %q, want %q", events[0].PPTFileName, "deck.pptx")
	}
	if events[0].PPTPPTXURL != "/api/projects/export-demo/exports/deck.pptx" {
		t.Fatalf("url = %q", events[0].PPTPPTXURL)
	}
}

func TestServeProjectAsset_FindsFilesInImagesAndTemplates(t *testing.T) {
	s := newTestServer(t)
	s.workspaceRoot = t.TempDir()
	s.frontRoot = t.TempDir()
	token, userID := createTestUserSession(t, s)

	projectName := "demo-project"
	projectRoot := filepath.Join(s.UserWorkspaceRoot(userID), projectName)
	if err := os.MkdirAll(filepath.Join(projectRoot, "images"), 0o755); err != nil {
		t.Fatalf("mkdir images: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}

	logoPath := filepath.Join(projectRoot, "images", "logo.png")
	if err := os.WriteFile(logoPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write logo: %v", err)
	}
	svgPath := filepath.Join(projectRoot, "templates", "01_cover.svg")
	if err := os.WriteFile(svgPath, []byte("<svg />"), 0o644); err != nil {
		t.Fatalf("write svg: %v", err)
	}

	router := NewRouter(s)

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectName+"/assets/logo.png", nil)
	req1.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("logo status = %d, want %d", w1.Code, http.StatusOK)
	}
	if body := w1.Body.String(); body != "png" {
		t.Fatalf("logo body = %q, want %q", body, "png")
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectName+"/assets/01_cover.svg", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("svg status = %d, want %d", w2.Code, http.StatusOK)
	}
	if body := w2.Body.String(); body != "<svg />" {
		t.Fatalf("svg body = %q, want %q", body, "<svg />")
	}
}

func createTestUserSession(t *testing.T, s *Server) (string, string) {
	t.Helper()
	auth, err := s.RegisterUser(vo.AuthReq{
		Username: "tester_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("RegisterUser() error = %v", err)
	}
	return auth.Token, auth.User.UserID
}

func TestGetPPTistProject_UsesGeneratedSlidesAndPreferredExport(t *testing.T) {
	s := newTestServer(t)
	s.workspaceRoot = t.TempDir()

	projectName := "demo-project"
	projectRoot := filepath.Join(s.workspaceRoot, projectName)
	if err := os.MkdirAll(filepath.Join(projectRoot, "svg_output"), 0o755); err != nil {
		t.Fatalf("mkdir svg_output: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "exports"), 0o755); err != nil {
		t.Fatalf("mkdir exports: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "svg_output", "01_cover.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatalf("write page1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "svg_output", "02_content.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatalf("write page2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "exports", "deck_svg.pptx"), []byte("svg"), 0o644); err != nil {
		t.Fatalf("write svg export: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "exports", "deck.pptx"), []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write editable export: %v", err)
	}

	result, err := s.GetPPTistProject(projectName)
	if err != nil {
		t.Fatalf("GetPPTistProject() error = %v", err)
	}

	slides, ok := result["slides"].([]any)
	if !ok || len(slides) != 2 {
		t.Fatalf("slides len = %v, want 2", len(slides))
	}
	if got := result["file_name"]; got != "deck.pptx" {
		t.Fatalf("file_name = %v, want deck.pptx", got)
	}
	if got := result["export_url"]; got != "/api/projects/demo-project/exports/deck.pptx" {
		t.Fatalf("export_url = %v", got)
	}
}

func TestGetPPTistProject_PrefersFinalSVGSlides(t *testing.T) {
	s := newTestServer(t)
	s.workspaceRoot = t.TempDir()

	projectName := "demo-project"
	projectRoot := filepath.Join(s.workspaceRoot, projectName)
	if err := os.MkdirAll(filepath.Join(projectRoot, "svg_output"), 0o755); err != nil {
		t.Fatalf("mkdir svg_output: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "svg_final"), 0o755); err != nil {
		t.Fatalf("mkdir svg_final: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "svg_output", "01_draft.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatalf("write output page: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "svg_final", "01_final.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatalf("write final page: %v", err)
	}

	result, err := s.GetPPTistProject(projectName)
	if err != nil {
		t.Fatalf("GetPPTistProject() error = %v", err)
	}
	slides, ok := result["slides"].([]any)
	if !ok || len(slides) != 1 {
		t.Fatalf("slides len = %v, want 1", len(slides))
	}
	first, ok := slides[0].(map[string]any)
	if !ok {
		t.Fatalf("slide type = %T, want map[string]any", slides[0])
	}
	if got := first["id"]; got != "lingxi-slide-001" {
		t.Fatalf("slide id = %v, want first final slide", got)
	}
	if got := result["page_count"]; got != 1 {
		t.Fatalf("page_count = %v, want 1", got)
	}
}

func TestGetPPTistProject_ConvertsSVGToEditableElements(t *testing.T) {
	s := newTestServer(t)
	s.workspaceRoot = t.TempDir()

	projectName := "editable-project"
	projectRoot := filepath.Join(s.workspaceRoot, projectName)
	if err := os.MkdirAll(filepath.Join(projectRoot, "svg_output"), 0o755); err != nil {
		t.Fatalf("mkdir svg_output: %v", err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720">
  <rect width="1280" height="720" fill="#111827"/>
  <rect x="100" y="100" width="300" height="120" fill="#6366F1"/>
  <text x="120" y="180" font-size="48" fill="#ffffff" font-weight="700">Hello SVG</text>
</svg>`
	if err := os.WriteFile(filepath.Join(projectRoot, "svg_output", "01_cover.svg"), []byte(svg), 0o644); err != nil {
		t.Fatalf("write svg: %v", err)
	}

	result, err := s.GetPPTistProject(projectName)
	if err != nil {
		t.Fatalf("GetPPTistProject() error = %v", err)
	}
	slides, ok := result["slides"].([]any)
	if !ok || len(slides) != 1 {
		t.Fatalf("slides len = %v, want 1", len(slides))
	}
	slide := slides[0].(map[string]any)
	background := slide["background"].(map[string]any)
	if got := background["color"]; got != "#111827" {
		t.Fatalf("background color = %v, want #111827", got)
	}
	elements := slide["elements"].([]map[string]any)
	if len(elements) != 2 {
		t.Fatalf("elements len = %d, want 2", len(elements))
	}
	if got := elements[0]["type"]; got != "shape" {
		t.Fatalf("first element type = %v, want shape", got)
	}
	if got := elements[1]["type"]; got != "text" {
		t.Fatalf("second element type = %v, want text", got)
	}
}

func TestSavePPTistDraft_AndGetPPTistProject_MergesNewGeneratedSlides(t *testing.T) {
	s := newTestServer(t)
	s.workspaceRoot = t.TempDir()

	projectName := "demo-project"
	projectRoot := filepath.Join(s.workspaceRoot, projectName)
	if err := os.MkdirAll(filepath.Join(projectRoot, "svg_output"), 0o755); err != nil {
		t.Fatalf("mkdir svg_output: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "svg_output", "01_cover.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatalf("write page1: %v", err)
	}

	draft := []byte(`{
  "title": "我的草稿",
  "width": 1000,
  "height": 562.5,
  "theme": {"fontColor":"#111"},
  "slides": [
    {
      "id": "custom-slide-1",
      "elements": [],
      "background": {"type": "solid", "color": "#fff"}
    }
  ]
}`)
	if err := s.SavePPTistDraft(projectName, draft); err != nil {
		t.Fatalf("SavePPTistDraft() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "svg_output", "02_content.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatalf("write page2: %v", err)
	}

	result, err := s.GetPPTistProject(projectName)
	if err != nil {
		t.Fatalf("GetPPTistProject() error = %v", err)
	}
	if got := result["title"]; got != "我的草稿" {
		t.Fatalf("title = %v, want 我的草稿", got)
	}
	if got := result["generated_only"]; got != false {
		t.Fatalf("generated_only = %v, want false", got)
	}

	slides, ok := result["slides"].([]any)
	if !ok || len(slides) != 2 {
		t.Fatalf("slides len = %v, want 2", len(slides))
	}
	first, ok := slides[0].(map[string]any)
	if !ok || first["id"] != "custom-slide-1" {
		t.Fatalf("first slide = %v, want custom-slide-1", slides[0])
	}
}

func TestGetPPTistProject_IgnoresBlankDraftSlideWhenGeneratedEditable(t *testing.T) {
	s := newTestServer(t)
	s.workspaceRoot = t.TempDir()

	projectName := "blank-draft-project"
	projectRoot := filepath.Join(s.workspaceRoot, projectName)
	if err := os.MkdirAll(filepath.Join(projectRoot, "svg_output"), 0o755); err != nil {
		t.Fatalf("mkdir svg_output: %v", err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720">
  <rect width="1280" height="720" fill="#111827"/>
  <text x="120" y="180" font-size="48" fill="#ffffff">Editable</text>
</svg>`
	if err := os.WriteFile(filepath.Join(projectRoot, "svg_output", "01_cover.svg"), []byte(svg), 0o644); err != nil {
		t.Fatalf("write svg: %v", err)
	}
	draft := []byte(`{
  "title": "空白草稿",
  "slides": [{
    "id": "blank-slide",
    "elements": [],
    "background": {"type": "solid", "color": "#ffffff"}
  }]
}`)
	if err := s.SavePPTistDraft(projectName, draft); err != nil {
		t.Fatalf("SavePPTistDraft() error = %v", err)
	}

	result, err := s.GetPPTistProject(projectName)
	if err != nil {
		t.Fatalf("GetPPTistProject() error = %v", err)
	}
	slides := result["slides"].([]any)
	slide := slides[0].(map[string]any)
	elements := slide["elements"].([]map[string]any)
	if len(elements) == 0 {
		t.Fatalf("elements len = 0, want generated editable elements")
	}
	if got := slide["id"]; got == "blank-slide" {
		t.Fatalf("blank draft slide was used instead of generated slide")
	}
}

func TestListPPTTemplates_ReturnsOrderedPreviewSVGURLs(t *testing.T) {
	s := newTestServer(t)
	s.pptMasterRoot = localPPTMasterRoot(t)

	templates, err := s.ListPPTTemplates()
	if err != nil {
		t.Fatalf("ListPPTTemplates() error = %v", err)
	}

	var target vo.PPTTemplateVO
	found := false
	for _, tpl := range templates {
		if tpl.Name == "academic_defense" {
			target = tpl
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("academic_defense template not found")
	}

	want := []string{
		"/api/ppt/templates/academic_defense/assets/01_cover.svg",
		"/api/ppt/templates/academic_defense/assets/02_toc.svg",
		"/api/ppt/templates/academic_defense/assets/02_chapter.svg",
		"/api/ppt/templates/academic_defense/assets/03_content.svg",
		"/api/ppt/templates/academic_defense/assets/04_ending.svg",
	}

	if target.PreviewSVGURL != want[0] {
		t.Fatalf("preview_svg_url = %q, want %q", target.PreviewSVGURL, want[0])
	}
	if len(target.PreviewSVGURLs) != len(want) {
		t.Fatalf("len(preview_svg_urls) = %d, want %d", len(target.PreviewSVGURLs), len(want))
	}
	if strings.Join(target.PreviewSVGURLs, ",") != strings.Join(want, ",") {
		t.Fatalf("preview_svg_urls = %v, want %v", target.PreviewSVGURLs, want)
	}
}

func TestOpenProjectExportInWPS_UsesExistingExportFile(t *testing.T) {
	s := newTestServer(t)
	s.workspaceRoot = t.TempDir()

	projectName := "demo-project"
	fileName := "demo.pptx"
	exportDir := filepath.Join(s.workspaceRoot, projectName, "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("mkdir exports: %v", err)
	}
	exportPath := filepath.Join(exportDir, fileName)
	if err := os.WriteFile(exportPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write export: %v", err)
	}

	t.Setenv("LINGXI_OPEN_PPT_COMMAND", "/usr/bin/true")

	if err := s.OpenProjectExportInWPS(projectName, fileName); err != nil {
		t.Fatalf("OpenProjectExportInWPS() error = %v", err)
	}
}

func TestOpenProjectExportInWPS_PrefersEditablePPTXWhenSVGVariantRequested(t *testing.T) {
	s := newTestServer(t)
	s.workspaceRoot = t.TempDir()

	projectName := "demo-project"
	exportDir := filepath.Join(s.workspaceRoot, projectName, "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("mkdir exports: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, "demo_svg.pptx"), []byte("svg"), 0o644); err != nil {
		t.Fatalf("write svg export: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, "demo.pptx"), []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write editable export: %v", err)
	}

	t.Setenv("LINGXI_OPEN_PPT_COMMAND", "/usr/bin/true")

	if err := s.OpenProjectExportInWPS(projectName, "demo_svg.pptx"); err != nil {
		t.Fatalf("OpenProjectExportInWPS() error = %v", err)
	}
}

func TestOpenProjectExportRoute_ReturnsOpened(t *testing.T) {
	s := newTestServer(t)
	s.workspaceRoot = t.TempDir()
	token, userID := createTestUserSession(t, s)

	projectName := "demo-project"
	fileName := "demo.pptx"
	exportDir := filepath.Join(s.UserWorkspaceRoot(userID), projectName, "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("mkdir exports: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, fileName), []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write export: %v", err)
	}

	t.Setenv("LINGXI_OPEN_PPT_COMMAND", "/usr/bin/true")

	router := NewRouter(s)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectName+"/exports/"+fileName+"/open", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result vo.R
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result.Code != 0 {
		t.Fatalf("response code = %d, want 0", result.Code)
	}
}

func localPPTMasterRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../ppt-master")
	if err != nil {
		t.Fatalf("resolve ppt-master root: %v", err)
	}
	return root
}
