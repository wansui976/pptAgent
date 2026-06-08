package server

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Conversation struct {
	ConversationID string `gorm:"primaryKey"`
	UserID         string `gorm:"index"`
	Title          string
	CreatedAt      int64
}

type User struct {
	UserID       string `gorm:"primaryKey"`
	Username     string `gorm:"uniqueIndex;size:64"`
	PasswordHash string
	CreatedAt    int64
}

type AuthSession struct {
	TokenHash  string `gorm:"primaryKey"`
	UserID     string `gorm:"index"`
	CreatedAt  int64
	ExpiresAt  int64 `gorm:"index"`
	LastUsedAt int64
}

type ChatMessage struct {
	MessageID       string `gorm:"primaryKey"`
	UserID          string `gorm:"index"`
	ConversationID  string `gorm:"index"`
	ParentMessageID string

	Query    string // 用户的原始提问
	Response string // 模型的最终输出
	Rounds   string // 用户提问到模型结束 tool loop 之间所有的 llm 请求，以 json 存储

	Model string // 使用的模型
	Usage string

	// PPTStage 是新流水线的状态快照（JSON 序列化的 PPTPipelineState）。
	// 老消息此列为空字符串，service 层视为 StageNone（走旧 SKILL.md 自驱）。
	PPTStage string

	CreatedAt int64
}

func InitDB(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	err = db.AutoMigrate(&User{}, &AuthSession{}, &Conversation{}, &ChatMessage{})
	if err != nil {
		return nil, err
	}
	return db, nil
}
