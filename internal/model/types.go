package model

import "time"

// ─────────────────────────────────────────────
//  角色常量
// ─────────────────────────────────────────────

const (
	RoleUser            = 0 // 用户
	RoleHeadCoach       = 1 // 主教练
	RoleAssistantCoach  = 2 // 副教练
	RoleCustomerService = 3 // 客服
)

// ─────────────────────────────────────────────
//  群聊消息
// ─────────────────────────────────────────────

// GroupMessage 来自后端接口的单条群聊消息。
type GroupMessage struct {
	UserID      int    `json:"userid"`
	UserName    string `json:"user_name"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
	Role        int    `json:"role"`         // 0=用户，1=主教练，2=副教练，3=客服
	CreatedTime string `json:"created_time"` // "2006-01-02 15:04:05"
}

// ParsedTime 将 CreatedTime 解析为本地时区的 time.Time。
func (m GroupMessage) ParsedTime() (time.Time, error) {
	return time.ParseInLocation("2006-01-02 15:04:05", m.CreatedTime, time.Local)
}

// IsUser 判断该消息是否由用户发送。
func (m GroupMessage) IsUser() bool { return m.Role == RoleUser }

// GroupMessagesPage 接口 GET .../groups/:id/message 的 data 字段。
type GroupMessagesPage struct {
	TotalPages  int            `json:"total_pages"`
	CurrentPage int            `json:"current_page"`
	Data        []GroupMessage `json:"data"`
}

// ─────────────────────────────────────────────
//  质检任务（放入 Channel 的工作单元）
// ─────────────────────────────────────────────

// AuditTask 是 Scheduler 生产、Worker 消费的最小任务单元。
type AuditTask struct {
	GroupID int
	Date    string // 格式 "20260602"
}

// ─────────────────────────────────────────────
//  AI 判定结果（LLM 结构化输出）
// ─────────────────────────────────────────────

// LLMJudgement 是要求大模型以 JSON 格式返回的定性判断结果。
type LLMJudgement struct {
	Resolved      bool        `json:"resolved"`        // 服务人员是否有效解决了用户问题
	Praised       bool        `json:"praised"`         // 是否检测到用户的表扬/点赞
	ServiceUserID int         `json:"service_user_id"` // 关联的服务人员 userid（不明确时填 0）
	ApplyUserID   int         `json:"apply_user_id"`   // 发出表扬/投诉的用户 userid（系统自动时填 0）
	Reason        string      `json:"reason"`          // 判定理由（≤100 字，将写入 complaint reason）
	Violations    []Violation `json:"violations"`      // 检测到的服务红线违规列表（无违规时为空数组）
}

// Violation 描述一条服务红线违规事件。
type Violation struct {
	RuleID        string `json:"rule_id"`         // 违规条款编号，如 "1a"、"5f"、"9s"
	ServiceUserID int    `json:"service_user_id"` // 违规的服务人员 userid（0=无法确定）
	Evidence      string `json:"evidence"`        // 违规证据原文（≤50字）
	Severity      string `json:"severity"`        // 违规等级: "一级" / "二级" / "三级"
	Description   string `json:"description"`     // 违规行为描述（≤50字）
}

// ─────────────────────────────────────────────
//  投诉/表扬请求（POST .../complaint/groups/:id）
// ─────────────────────────────────────────────

// ComplaintType 对应后端 type 字段。
type ComplaintType int

const (
	ComplaintPositive ComplaintType = 1 // 正向激励（表扬 / 经验）
	ComplaintWarning  ComplaintType = 2 // 改进提醒（警告）
)

// ComplaintSource 对应后端 source 字段。
type ComplaintSource int

const (
	SourceAIUser    ComplaintSource = 3 // ai-用户（用户触发的表扬）
	SourceAISystem  ComplaintSource = 4 // ai-系统（系统自动生成）
	SourceAIService ComplaintSource = 5 // ai-服务人员
)

// ComplaintReq 是提交投诉/表扬的 Body 结构。
type ComplaintReq struct {
	Type          ComplaintType   `json:"type"`
	ServiceUserID int             `json:"service_user_id"`
	ApplyUserID   int             `json:"apply_user_id"`
	Reason        string          `json:"reason"`
	Pic           []string        `json:"pic"`
	Source        ComplaintSource `json:"source"`
}
