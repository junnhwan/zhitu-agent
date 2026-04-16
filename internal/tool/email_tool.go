package tool

import (
	"context"
	"fmt"
	"log"
	"net/smtp"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

// EmailToolInput is the input for sendEmail.
type EmailToolInput struct {
	TargetEmail string `json:"targetEmail" jsonschema:"description=接收人的邮箱地址"`
	Subject     string `json:"subject"     jsonschema:"description=邮件标题"`
	Content     string `json:"content"    jsonschema:"description=邮件正文内容"`
}

// NewEmailTool creates a tool that sends emails via SMTP.
// Mirrors Java EmailTool — @Tool("向特定用户发送电子邮件。")
func NewEmailTool(cfg *config.MailConfig) (tool.InvokableTool, error) {
	return utils.InferTool[EmailToolInput, string](
		"sendEmail",
		"向特定用户发送电子邮件。需要提供接收人邮箱地址、邮件标题和邮件正文内容。",
		func(ctx context.Context, input EmailToolInput) (string, error) {
			return sendEmail(cfg, input.TargetEmail, input.Subject, input.Content), nil
		},
	)
}

// sendEmail sends a simple text email via SMTP.
// Mirrors Java EmailTool.sendEmail — SimpleMailMessage with from/to/subject/text.
func sendEmail(cfg *config.MailConfig, targetEmail, subject, content string) string {
	log.Printf("[EmailTool] sending email -> To: %s, Subject: %s", targetEmail, subject)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// Build email message
	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n",
		cfg.Username, targetEmail, subject, content)

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)

	if err := smtp.SendMail(addr, auth, cfg.Username, []string{targetEmail}, []byte(msg)); err != nil {
		log.Printf("[EmailTool] send failed: %v", err)
		return fmt.Sprintf("邮件发送失败: %v", err)
	}

	log.Printf("[EmailTool] email sent successfully")
	return fmt.Sprintf("邮件已成功发送给 %s", targetEmail)
}