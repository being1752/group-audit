package audit

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"golang.org/x/time/rate"

	"group-audit/internal/ai"
	"group-audit/internal/model"
)

// baseSystemPrompt 是不含红线文档时的基础系统提示。
const baseSystemPrompt = `你是一个专业的客服质检助手，负责分析群聊记录并评判服务质量。
你需要完成两类评判：
1. 基础服务质量：问题是否被有效解决、是否有用户表扬
2. 服务红线违规检测：若系统提示末尾附有《服务红线》文档，请逐条排查违规行为

请严格按照以下 JSON 格式输出评判结果，不要包含任何其他内容或 markdown 标记：
{
  "resolved": true,
  "praised": false,
  "service_user_id": 0,
  "apply_user_id": 0,
  "reason": "基础质量判定理由，不超过100字",
  "violations": [
    {
      "rule_id": "5f",
      "service_user_id": 201,
      "evidence": "违规原文（≤50字）",
      "severity": "一级",
      "description": "违规行为描述（≤50字）"
    }
  ]
}
字段说明：
- resolved: 服务人员是否有效解决了用户提出的问题（没有明确问题时填 true）
- praised: 用户是否有明显的表扬、点赞、感谢等正向情感表达
- service_user_id: 本次对话中最主要的服务人员 userid（无法确定时填 0）
- apply_user_id: 发出表扬的用户 userid（仅 praised=true 时有意义，否则填 0）
- reason: 简要说明基础质量判定依据（≤100字）
- violations: 检测到的服务红线违规列表，无违规时填空数组 []
  - rule_id: 违规条款编号，如 "1a"、"5f"、"9s"
  - service_user_id: 违规的服务人员 userid（0=无法确定）
  - evidence: 违规的原文片段（≤50字）
  - severity: 违规等级，必须是 "一级" / "二级" / "三级" 之一
  - description: 违规行为的简要描述（≤50字）`

// buildSystemPrompt 构建最终系统提示。
// 若 redlineContent 非空，将红线文档全文附加在后面，供 AI 逐条对照排查。
func buildSystemPrompt(redlineContent string) string {
	if redlineContent == "" {
		return baseSystemPrompt
	}
	return baseSystemPrompt + "\n\n---\n\n# 服务红线文档（用于违规检测依据，请逐条排查）\n\n" + redlineContent
}

// QualResult 定性分析结果，每个条目对应一条需要提交的投诉/表扬。
type QualResult struct {
	Complaints []model.ComplaintReq
}

// Qualitative 对群聊消息列表执行基于 LLM 的定性分析。
//
//   - redlineContent: 服务红线文档全文，为空时跳过红线违规检测。
//   - maxMessages: 传入 LLM 的消息上限，超出时截断取首尾。
//   - limiter: 全局 LLM 速率限制器，调用前会阻塞等待令牌。
func Qualitative(ctx context.Context, provider ai.Provider, limiter *rate.Limiter, messages []model.GroupMessage, redlineContent string, maxMessages int) (QualResult, error) {
	if len(messages) == 0 {
		return QualResult{}, nil
	}

	truncated := truncateMessages(messages, maxMessages)
	sysPrompt := buildSystemPrompt(redlineContent)
	userPrompt := buildPrompt(truncated)

	// 打印发送给 AI 的 Prompt（调试用）
	log := slog.With("msg_count", len(truncated), "total", len(messages))
	log.Debug("──── AI 分析开始 ────")
	log.Debug("发送给 AI 的消息内容")
	for _, line := range strings.Split(userPrompt, "\n") {
		log.Debug(fmt.Sprintf("  | %s", line))
	}
	log.Debug("───────────────────")

	// 全局限流：等待令牌桶允许
	if err := limiter.Wait(ctx); err != nil {
		return QualResult{}, fmt.Errorf("限流等待被取消: %w", err)
	}

	var judgement model.LLMJudgement
	if err := provider.ChatJSON(ctx, sysPrompt, userPrompt, &judgement); err != nil {
		return QualResult{}, fmt.Errorf("LLM 调用失败: %w", err)
	}

	// 构建 userID → userName 映射，用于违规日志
	nameMap := make(map[int]string, len(messages))
	for _, m := range messages {
		nameMap[m.UserID] = m.UserName
	}

	// 打印 AI 原始判定结果
	log.Info("AI 判定结果",
		"resolved", judgement.Resolved,
		"praised", judgement.Praised,
		"service_user_id", judgement.ServiceUserID,
		"apply_user_id", judgement.ApplyUserID,
		"reason", judgement.Reason,
		"violations", len(judgement.Violations),
	)
	for i, v := range judgement.Violations {
		userName := nameMap[v.ServiceUserID]
		log.Warn(fmt.Sprintf("  红线违规[%d] 条款%s | 等级%s | 说话人:%s(%d) | 原文:%s | 说明:%s",
			i+1, v.RuleID, v.Severity, userName, v.ServiceUserID, v.Evidence, v.Description))
	}

	return buildComplaints(judgement), nil
}

// buildComplaints 根据 LLMJudgement 生成对应的投诉/表扬条目。
//
// 映射规则：
//   - praised=true       → 表扬（type=1, source=3 ai-用户）
//   - resolved=true      → 经验（type=1, source=4 ai-系统）
//   - resolved=false     → 警告（type=2, source=4 ai-系统）
//   - violations 非空    → 每条红线违规生成一条警告（type=2, source=4 ai-系统）
func buildComplaints(j model.LLMJudgement) QualResult {
	var complaints []model.ComplaintReq

	if j.Praised {
		complaints = append(complaints, model.ComplaintReq{
			Type:          model.ComplaintPositive,
			ServiceUserID: j.ServiceUserID,
			ApplyUserID:   j.ApplyUserID,
			Reason:        "[表扬] " + j.Reason,
			Pic:           []string{},
			Source:        model.SourceAIUser,
		})
	}

	if j.Resolved {
		complaints = append(complaints, model.ComplaintReq{
			Type:          model.ComplaintPositive,
			ServiceUserID: j.ServiceUserID,
			ApplyUserID:   0,
			Reason:        "[经验] " + j.Reason,
			Pic:           []string{},
			Source:        model.SourceAISystem,
		})
	} else {
		complaints = append(complaints, model.ComplaintReq{
			Type:          model.ComplaintWarning,
			ServiceUserID: j.ServiceUserID,
			ApplyUserID:   0,
			Reason:        "[警告] " + j.Reason,
			Pic:           []string{},
			Source:        model.SourceAISystem,
		})
	}

	// 红线违规：每条独立提交，方便后端分条追溯
	for _, v := range j.Violations {
		reason := fmt.Sprintf("[红线违规-%s][条款%s] %s | 原文：%s",
			v.Severity, v.RuleID, v.Description, v.Evidence)
		complaints = append(complaints, model.ComplaintReq{
			Type:          model.ComplaintWarning,
			ServiceUserID: v.ServiceUserID,
			ApplyUserID:   0,
			Reason:        reason,
			Pic:           []string{},
			Source:        model.SourceAISystem,
		})
	}

	return QualResult{Complaints: complaints}
}

// buildPrompt 将消息列表格式化为适合 LLM 理解的纯文本。
func buildPrompt(messages []model.GroupMessage) string {
	var sb strings.Builder
	sb.WriteString("以下是待分析的群聊记录（格式：[时间] 角色-用户名(userid): 内容）：\n\n")

	for _, m := range messages {
		sb.WriteString(fmt.Sprintf("[%s] %s-%s(%d): %s\n",
			m.CreatedTime, roleLabel(m.Role), m.UserName, m.UserID, m.Content))
	}

	sb.WriteString("\n请按照系统指令中的 JSON 格式返回评判结果。")
	return sb.String()
}

// roleLabel 将角色数值转为 AI 可识别的中文标签。
func roleLabel(role int) string {
	switch role {
	case model.RoleUser:
		return "用户"
	case model.RoleHeadCoach:
		return "主教练"
	case model.RoleAssistantCoach:
		return "副教练"
	case model.RoleCustomerService:
		return "客服"
	default:
		return fmt.Sprintf("未知(%d)", role)
	}
}

// truncateMessages 截断消息列表至 maxN 条，超出时保留前半和后半。
func truncateMessages(messages []model.GroupMessage, maxN int) []model.GroupMessage {
	if len(messages) <= maxN {
		return messages
	}
	half := maxN / 2
	return append(messages[:half:half], messages[len(messages)-half:]...)
}
