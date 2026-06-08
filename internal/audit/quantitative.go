package audit

import (
	"fmt"
	"group-audit/internal/model"
)

// QuantResult 定量分析结果，每个条目对应一条需要提交的投诉/表扬。
type QuantResult struct {
	Complaints []model.ComplaintReq
}

// SlowResponse 描述一次响应超时事件。
type SlowResponse struct {
	UserMsg    model.GroupMessage
	ServiceMsg model.GroupMessage
	DelayMin   float64
}

// Quantitative 对群聊消息列表执行纯规则的定量分析：
//
//  1. 统计每位服务人员的消息总数，若所有服务人员合计消息数为 0 → 生成警告。
//  2. 扫描消息序列，计算用户提问后首条服务人员回复的时间差，超过 timeoutMin 分钟 → 生成警告。
func Quantitative(messages []model.GroupMessage, timeoutMin float64) QuantResult {
	if len(messages) == 0 {
		return QuantResult{}
	}

	var complaints []model.ComplaintReq

	// ── 规则1：服务人员消息数 ──────────────────────────────
	serviceCount := map[int]int{} // userid → 消息数
	for _, m := range messages {
		if !m.IsUser() {
			serviceCount[m.UserID]++
		}
	}

	totalServiceMsg := 0
	for _, cnt := range serviceCount {
		totalServiceMsg += cnt
	}

	if totalServiceMsg == 0 {
		complaints = append(complaints, model.ComplaintReq{
			Type:          model.ComplaintWarning,
			ServiceUserID: 0,
			ApplyUserID:   0,
			Reason:        "当日所有服务人员在本群聊中未发送任何消息",
			Pic:           []string{},
			Source:        model.SourceAISystem,
		})
	}

	// ── 规则2：响应超时 ────────────────────────────────────
	slowResponses := detectSlowResponses(messages, timeoutMin)
	for _, sr := range slowResponses {
		complaints = append(complaints, model.ComplaintReq{
			Type:          model.ComplaintWarning,
			ServiceUserID: sr.ServiceMsg.UserID,
			ApplyUserID:   sr.UserMsg.UserID,
			Reason: fmt.Sprintf("用户 %s 于 %s 发送消息后，服务人员 %s 响应延迟 %.1f 分钟（超过阈值 %.1f 分钟）",
				sr.UserMsg.UserName, sr.UserMsg.CreatedTime,
				sr.ServiceMsg.UserName, sr.DelayMin, timeoutMin),
			Pic:    []string{},
			Source: model.SourceAISystem,
		})
	}

	return QuantResult{Complaints: complaints}
}

// detectSlowResponses 扫描消息序列，找出所有响应超时的场景。
// 算法：遇到用户消息，记录时间；遇到服务人员消息，计算时间差；超时则记录。
func detectSlowResponses(messages []model.GroupMessage, timeoutMin float64) []SlowResponse {
	var results []SlowResponse

	var pendingUserMsg *model.GroupMessage

	for i := range messages {
		msg := messages[i]
		if msg.IsUser() {
			// 更新待回复的用户消息（若已有未被回复的，覆盖为最新问题）
			pendingUserMsg = &messages[i]
			continue
		}

		// 服务人员消息
		if pendingUserMsg == nil {
			continue
		}

		userTime, err1 := pendingUserMsg.ParsedTime()
		svcTime, err2 := msg.ParsedTime()
		if err1 != nil || err2 != nil {
			// 时间解析失败，跳过本次检测
			pendingUserMsg = nil
			continue
		}

		delayMin := svcTime.Sub(userTime).Minutes()
		if delayMin > timeoutMin {
			results = append(results, SlowResponse{
				UserMsg:    *pendingUserMsg,
				ServiceMsg: msg,
				DelayMin:   delayMin,
			})
		}
		// 服务人员已响应，重置待回复消息
		pendingUserMsg = nil
	}

	return results
}
