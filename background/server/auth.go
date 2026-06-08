package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"lingxi/background/vo"
)

const (
	authContextUserID   = "auth_user_id"
	authContextUsername = "auth_username"
	authContextToken    = "auth_token"
	sessionTTL          = 30 * 24 * time.Hour
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,64}$`)

func (s *Server) RegisterUser(req vo.AuthReq) (vo.AuthResp, error) {
	username, password, err := normalizeAuthInput(req)
	if err != nil {
		return vo.AuthResp{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return vo.AuthResp{}, err
	}

	now := time.Now().Unix()
	user := User{
		UserID:       uuid.NewString(),
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    now,
	}
	if err := s.db.Create(&user).Error; err != nil {
		return vo.AuthResp{}, fmt.Errorf("账号已存在或无法创建")
	}

	return s.createAuthSession(user)
}

func (s *Server) LoginUser(req vo.AuthReq) (vo.AuthResp, error) {
	username, password, err := normalizeAuthInput(req)
	if err != nil {
		return vo.AuthResp{}, err
	}

	var user User
	if err := s.db.First(&user, "username = ?", username).Error; err != nil {
		return vo.AuthResp{}, fmt.Errorf("账号或密码不正确")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return vo.AuthResp{}, fmt.Errorf("账号或密码不正确")
	}

	return s.createAuthSession(user)
}

func (s *Server) LogoutToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return s.db.Delete(&AuthSession{}, "token_hash = ?", hashToken(token)).Error
}

func (s *Server) AuthenticateToken(token string) (User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return User{}, errors.New("missing token")
	}

	now := time.Now().Unix()
	var session AuthSession
	if err := s.db.First(&session, "token_hash = ?", hashToken(token)).Error; err != nil {
		return User{}, errors.New("invalid token")
	}
	if session.ExpiresAt <= now {
		_ = s.db.Delete(&AuthSession{}, "token_hash = ?", session.TokenHash).Error
		return User{}, errors.New("token expired")
	}

	var user User
	if err := s.db.First(&user, "user_id = ?", session.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = s.db.Delete(&AuthSession{}, "token_hash = ?", session.TokenHash).Error
		}
		return User{}, errors.New("invalid token")
	}

	_ = s.db.Model(&AuthSession{}).
		Where("token_hash = ?", session.TokenHash).
		Update("last_used_at", now).Error

	return user, nil
}

func normalizeAuthInput(req vo.AuthReq) (string, string, error) {
	username := strings.ToLower(strings.TrimSpace(req.Username))
	password := strings.TrimSpace(req.Password)
	if !usernamePattern.MatchString(username) {
		return "", "", fmt.Errorf("账号需为 3-64 位字母、数字、点、下划线或连字符")
	}
	if len([]rune(password)) < 8 {
		return "", "", fmt.Errorf("密码至少 8 位")
	}
	return username, password, nil
}

func (s *Server) createAuthSession(user User) (vo.AuthResp, error) {
	token, err := randomToken()
	if err != nil {
		return vo.AuthResp{}, err
	}
	now := time.Now()
	expiresAt := now.Add(sessionTTL).Unix()
	session := AuthSession{
		TokenHash:  hashToken(token),
		UserID:     user.UserID,
		CreatedAt:  now.Unix(),
		ExpiresAt:  expiresAt,
		LastUsedAt: now.Unix(),
	}
	if err := s.db.Create(&session).Error; err != nil {
		return vo.AuthResp{}, err
	}
	return vo.AuthResp{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      userVO(user),
	}, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func userVO(user User) vo.AuthUserVO {
	return vo.AuthUserVO{
		UserID:    user.UserID,
		Username:  user.Username,
		CreatedAt: user.CreatedAt,
	}
}

func extractAuthToken(c *gin.Context) string {
	if raw := strings.TrimSpace(c.GetHeader("Authorization")); raw != "" {
		if token, ok := strings.CutPrefix(raw, "Bearer "); ok {
			return strings.TrimSpace(token)
		}
	}
	if token := strings.TrimSpace(c.Query("auth_token")); token != "" {
		return token
	}
	return ""
}

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractAuthToken(c)
		user, err := s.AuthenticateToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, vo.Err(http.StatusUnauthorized, "请先登录"))
			return
		}
		c.Set(authContextUserID, user.UserID)
		c.Set(authContextUsername, user.Username)
		c.Set(authContextToken, token)
		c.Next()
	}
}

func currentUserID(c *gin.Context) string {
	userID, _ := c.Get(authContextUserID)
	return strings.TrimSpace(fmt.Sprint(userID))
}

func currentAuthToken(c *gin.Context) string {
	token, _ := c.Get(authContextToken)
	return strings.TrimSpace(fmt.Sprint(token))
}
