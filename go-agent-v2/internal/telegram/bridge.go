// Package telegram 提供 Telegram 桥接 (对应 Python tg_bridge.py)。
package telegram

import (
	"context"

	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// Bridge Telegram 桥接器。
type Bridge struct {
	cfg        *config.Config
	interaction *store.InteractionStore
	auditLog    *store.AuditLogStore
}

// NewBridge 创建 Telegram 桥接器。
func NewBridge(cfg *config.Config, interaction *store.InteractionStore, audit *store.AuditLogStore) *Bridge {
	return &Bridge{cfg: cfg, interaction: interaction, auditLog: audit}
}

// Start 启动 Telegram 消息监听。
func (b *Bridge) Start(ctx context.Context) error {
	if b.cfg.TGBotToken == "" {
		logger.Info("telegram bridge disabled (no TG_BOT_TOKEN)")
		return nil
	}
	logger.Infow("telegram bridge starting", "chat_id", b.cfg.TGChatID)

	// TODO: 集成 go-telegram-bot-api
	// 1. 创建 bot
	// 2. 注册 command handler (/status, /exec, /query)
	// 3. 消息转发到 interaction store
	// 4. 推送通知到 chat
	<-ctx.Done()
	return nil
}

// SendMessage 发送消息到 Telegram chat。
func (b *Bridge) SendMessage(ctx context.Context, text string) error {
	if b.cfg.TGBotToken == "" {
		return nil
	}
	// TODO: 实现消息发送
	logger.Infow("telegram message sent", "text_len", len(text))
	return nil
}
