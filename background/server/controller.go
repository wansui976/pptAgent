package server

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"lingxi/background/agent"
	"lingxi/background/shared/log"
	"lingxi/background/vo"
)

func NewRouter(s *Server) *gin.Engine {
	g := gin.Default()
	g.Use(corsMiddleware())

	api := g.Group("/api")
	api.POST("/auth/register", s.registerUser)
	api.POST("/auth/login", s.loginUser)
	api.GET("/ppt/templates", s.listPPTTemplates)
	api.GET("/ppt/templates/:template_name/assets/*filepath", s.servePPTTemplateAsset)

	protected := api.Group("/")
	protected.Use(s.authMiddleware())
	protected.GET("/auth/me", s.currentUser)
	protected.POST("/auth/logout", s.logoutUser)
	protected.POST("/conversation", s.createConversation)
	protected.GET("/conversation", s.listConversations)
	protected.PATCH("/conversation/:conversation_id", s.renameConversation)
	protected.DELETE("/conversation/:conversation_id", s.deleteConversation)
	protected.POST("/conversation/:conversation_id/message", s.createMessage)
	protected.GET("/conversation/:conversation_id/message", s.listMessages)
	protected.GET("/projects/:project_name/exports/:file", s.downloadProjectExport)
	protected.POST("/projects/:project_name/exports/:file/open", s.openProjectExportInWPS)
	protected.GET("/projects/:project_name/assets/*filepath", s.serveProjectAsset)
	protected.GET("/projects/:project_name/pptist", s.getPPTistProject)
	protected.POST("/projects/:project_name/pptist", s.savePPTistProject)

	g.GET("/", s.serveFrontendIndex)
	g.GET("/index.html", s.serveFrontendIndex)
	g.GET("/editor", s.serveFrontendEditor)
	g.GET("/editor.html", s.serveFrontendEditor)
	g.Static("/css", filepath.Join(s.frontRoot, "css"))
	g.Static("/js", filepath.Join(s.frontRoot, "js"))
	g.Static("/assets", filepath.Join(s.frontRoot, "assets"))
	g.Static("/pptist", s.pptistRoot)
	g.NoRoute(s.serveFrontendFallback)

	return g
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if isAllowedOrigin(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept, Cache-Control")
		c.Header("Access-Control-Expose-Headers", "Content-Disposition")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	return strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "http://127.0.0.1:") ||
		strings.HasPrefix(origin, "http://[::1]:")
}

// POST /auth/register
func (s *Server) registerUser(c *gin.Context) {
	var req vo.AuthReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, vo.Err(400, err.Error()))
		return
	}
	result, err := s.RegisterUser(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, vo.Err(400, err.Error()))
		return
	}
	c.JSON(http.StatusOK, vo.OK(result))
}

// POST /auth/login
func (s *Server) loginUser(c *gin.Context) {
	var req vo.AuthReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, vo.Err(400, err.Error()))
		return
	}
	result, err := s.LoginUser(req)
	if err != nil {
		c.JSON(http.StatusUnauthorized, vo.Err(401, err.Error()))
		return
	}
	c.JSON(http.StatusOK, vo.OK(result))
}

// GET /auth/me
func (s *Server) currentUser(c *gin.Context) {
	userID := currentUserID(c)
	var user User
	if err := s.db.First(&user, "user_id = ?", userID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, vo.Err(401, "请先登录"))
		return
	}
	c.JSON(http.StatusOK, vo.OK(userVO(user)))
}

// POST /auth/logout
func (s *Server) logoutUser(c *gin.Context) {
	if err := s.LogoutToken(currentAuthToken(c)); err != nil {
		c.JSON(http.StatusInternalServerError, vo.Err(500, err.Error()))
		return
	}
	c.JSON(http.StatusOK, vo.OK(map[string]any{"logged_out": true}))
}

// POST /conversation
func (s *Server) createConversation(c *gin.Context) {
	var req vo.CreateConversationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, vo.Err(400, err.Error()))
		return
	}
	req.UserID = currentUserID(c)

	result, err := s.CreateConversation(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, vo.Err(500, err.Error()))
		return
	}

	c.JSON(http.StatusOK, vo.OK(result))
}

// GET /conversation
func (s *Server) listConversations(c *gin.Context) {
	userID := currentUserID(c)

	result, err := s.ListConversations(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, vo.Err(500, err.Error()))
		return
	}

	c.JSON(http.StatusOK, vo.OK(result))
}

// PATCH /conversation/:conversation_id
func (s *Server) renameConversation(c *gin.Context) {
	conversationID := c.Param("conversation_id")

	var req vo.UpdateConversationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, vo.Err(400, err.Error()))
		return
	}

	result, err := s.RenameConversationForUser(currentUserID(c), conversationID, req.Title)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, vo.Err(404, "conversation not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, vo.Err(500, err.Error()))
		return
	}

	c.JSON(http.StatusOK, vo.OK(result))
}

// DELETE /conversation/:conversation_id
func (s *Server) deleteConversation(c *gin.Context) {
	conversationID := c.Param("conversation_id")

	if err := s.DeleteConversationForUser(currentUserID(c), conversationID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, vo.Err(404, "conversation not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, vo.Err(500, err.Error()))
		return
	}

	c.JSON(http.StatusOK, vo.OK(map[string]any{"conversation_id": conversationID}))
}

// GET /conversation/:conversation_id/message
func (s *Server) listMessages(c *gin.Context) {
	conversationID := c.Param("conversation_id")

	result, err := s.ListMessagesForUser(currentUserID(c), conversationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, vo.Err(404, "conversation not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, vo.Err(500, err.Error()))
		return
	}

	c.JSON(http.StatusOK, vo.OK(result))
}

// POST /conversation/:conversation_id/message
// 创建新消息并 SSE 流式输出 agent 响应
func (s *Server) createMessage(c *gin.Context) {
	conversationID := c.Param("conversation_id")

	var req vo.CreateMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, vo.Err(400, err.Error()))
		return
	}
	req.UserID = currentUserID(c)

	eventCh := make(chan vo.SSEMessageVO, 64)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	go func() {
		defer close(eventCh)
		if err := s.CreateMessage(c.Request.Context(), conversationID, req, eventCh); err != nil {
			errMsg := err.Error()
			eventCh <- vo.SSEMessageVO{Event: agent.EventError, Content: &errMsg}
			return
		}
	}()

	for {
		select {
		case <-c.Request.Context().Done():
			log.Warn("Server is shutting down. Exiting...")
			return
		case e, ok := <-eventCh:
			if !ok {
				return
			}
			c.SSEvent("message", e)
			c.Writer.Flush()
		}
	}
}

// GET /projects/:project_name/exports/:file
func (s *Server) downloadProjectExport(c *gin.Context) {
	projectName := filepath.Base(c.Param("project_name"))
	fileName := filepath.Base(c.Param("file"))
	workspaceRoot := s.UserWorkspaceRoot(currentUserID(c))
	exportPath := filepath.Join(workspaceRoot, projectName, "exports", fileName)
	log.Warnf("download export: workspaceRoot=%s project=%s file=%s path=%s", workspaceRoot, projectName, fileName, exportPath)
	if info, err := os.Stat(exportPath); err != nil || info.IsDir() {
		if err != nil {
			log.Warnf("download export stat failed: %v", err)
		}
		c.JSON(http.StatusNotFound, vo.Err(404, "export file not found"))
		return
	}

	c.FileAttachment(exportPath, fileName)
}

// POST /projects/:project_name/exports/:file/open
func (s *Server) openProjectExportInWPS(c *gin.Context) {
	projectName := c.Param("project_name")
	fileName := c.Param("file")

	if err := s.OpenProjectExportInWPSForUser(currentUserID(c), projectName, fileName); err != nil {
		c.JSON(http.StatusBadRequest, vo.Err(400, err.Error()))
		return
	}

	c.JSON(http.StatusOK, vo.OK(map[string]any{
		"project_name": filepath.Base(projectName),
		"file_name":    filepath.Base(fileName),
		"opened":       true,
	}))
}

// GET /ppt/templates
func (s *Server) listPPTTemplates(c *gin.Context) {
	templates, err := s.ListPPTTemplates()
	if err != nil {
		c.JSON(http.StatusInternalServerError, vo.Err(500, err.Error()))
		return
	}
	c.JSON(http.StatusOK, vo.OK(templates))
}

// GET /ppt/templates/:template_name/assets/*filepath
func (s *Server) servePPTTemplateAsset(c *gin.Context) {
	templateName := filepath.Base(c.Param("template_name"))
	requestedPath := strings.TrimPrefix(c.Param("filepath"), "/")
	assetPath, err := resolveTemplateAssetPath(s.pptTemplateLayoutsDir(), templateName, requestedPath)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		c.JSON(status, vo.Err(status, err.Error()))
		return
	}

	info, err := os.Stat(assetPath)
	if err != nil || info.IsDir() {
		c.JSON(http.StatusNotFound, vo.Err(404, "template asset not found"))
		return
	}

	c.File(assetPath)
}

// GET /projects/:project_name/assets/*filepath
func (s *Server) serveProjectAsset(c *gin.Context) {
	projectName := filepath.Base(c.Param("project_name"))
	projectRoot := filepath.Join(s.UserWorkspaceRoot(currentUserID(c)), projectName)

	requestedPath := strings.TrimPrefix(c.Param("filepath"), "/")
	assetPath, err := resolveProjectAssetPath(projectRoot, requestedPath)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		c.JSON(status, vo.Err(status, err.Error()))
		return
	}

	info, err := os.Stat(assetPath)
	if err != nil || info.IsDir() {
		c.JSON(http.StatusNotFound, vo.Err(404, "asset file not found"))
		return
	}

	c.File(assetPath)
}

// GET /projects/:project_name/pptist
func (s *Server) getPPTistProject(c *gin.Context) {
	projectName := c.Param("project_name")
	result, err := s.GetPPTistProjectForUser(currentUserID(c), projectName)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, vo.Err(status, err.Error()))
		return
	}
	attachAuthTokenToProjectURLs(result, currentAuthToken(c))
	c.JSON(http.StatusOK, vo.OK(result))
}

// POST /projects/:project_name/pptist
func (s *Server) savePPTistProject(c *gin.Context) {
	projectName := c.Param("project_name")
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, vo.Err(400, err.Error()))
		return
	}
	if err := s.SavePPTistDraftForUser(currentUserID(c), projectName, body); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, vo.Err(status, err.Error()))
		return
	}
	c.JSON(http.StatusOK, vo.OK(map[string]any{
		"project_name": filepath.Base(projectName),
		"saved":        true,
	}))
}

func resolveProjectAssetPath(projectRoot string, requestedPath string) (string, error) {
	if strings.TrimSpace(requestedPath) == "" {
		return "", errors.New("asset path is required")
	}

	cleaned := filepath.Clean(strings.TrimPrefix(requestedPath, "/"))
	if cleaned == "." || cleaned == "" {
		return "", errors.New("asset path is required")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid asset path")
	}

	candidates := make([]string, 0, 4)
	if strings.Contains(cleaned, string(filepath.Separator)) {
		candidates = append(candidates, filepath.Join(projectRoot, cleaned))
	} else {
		candidates = append(candidates,
			filepath.Join(projectRoot, "images", cleaned),
			filepath.Join(projectRoot, "templates", cleaned),
			filepath.Join(projectRoot, cleaned),
		)
	}

	for _, candidate := range candidates {
		rel, err := filepath.Rel(projectRoot, candidate)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", os.ErrNotExist
}

func resolveTemplateAssetPath(layoutsDir string, templateName string, requestedPath string) (string, error) {
	if strings.TrimSpace(templateName) == "" {
		return "", errors.New("template name is required")
	}
	if strings.TrimSpace(requestedPath) == "" {
		return "", errors.New("asset path is required")
	}

	templateRoot := filepath.Join(layoutsDir, templateName)
	cleaned := filepath.Clean(strings.TrimPrefix(requestedPath, "/"))
	if cleaned == "." || cleaned == "" {
		return "", errors.New("asset path is required")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid asset path")
	}

	candidate := filepath.Join(templateRoot, cleaned)
	rel, err := filepath.Rel(templateRoot, candidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid asset path")
	}
	if _, err := os.Stat(candidate); err != nil {
		return "", os.ErrNotExist
	}
	return candidate, nil
}

func (s *Server) serveFrontendIndex(c *gin.Context) {
	indexPath := filepath.Join(s.frontRoot, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		c.String(http.StatusNotFound, "frontend index not found")
		return
	}
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.File(indexPath)
}

func (s *Server) serveFrontendEditor(c *gin.Context) {
	values := url.Values{}
	if projectName := strings.TrimSpace(c.Query("project")); projectName != "" {
		values.Set("project", projectName)
	}
	if authToken := strings.TrimSpace(c.Query("auth_token")); authToken != "" {
		values.Set("auth_token", authToken)
	}
	target := "/pptist/index.html"
	if encoded := values.Encode(); encoded != "" {
		target = target + "?" + encoded
	}
	c.Redirect(http.StatusFound, target)
}

func (s *Server) serveFrontendFallback(c *gin.Context) {
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api/") {
		c.JSON(http.StatusNotFound, vo.Err(404, "api route not found"))
		return
	}
	if path == "/editor" || path == "/editor.html" {
		s.serveFrontendEditor(c)
		return
	}
	if strings.HasPrefix(path, "/css/") || strings.HasPrefix(path, "/js/") || strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/pptist/") {
		c.Status(http.StatusNotFound)
		return
	}
	s.serveFrontendIndex(c)
}
