// Package mock 提供本地测试用的假数据实现，无需连接任何真实后端或 AI 服务。
//
// 预置场景（group_id → 场景）：
//
//	1 → 用户问题被有效解决      → 定性：经验
//	2 → 用户明确点赞表扬        → 定性：表扬 + 经验
//	3 → 服务人员未有效解决问题  → 定性：警告
//	4 → 服务人员响应超时(>5min) → 定量：响应超时警告
//	5 → 全天无服务人员消息      → 定量：零消息警告
//	6 → 制造焦虑 + 治病话术     → 红线违规：条款3(三级) + 条款5f(一级)
//	7 → 强制指令 + 擅自要求停药 → 红线违规：条款6h(三级) + 条款7o(一级)
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"group-audit/internal/model"
)

// ─────────────────────────────────────────────
//  GroupClient（实现 client.GroupFetcher）
// ─────────────────────────────────────────────

// GroupClient 返回预置的群聊 ID 列表和消息记录。
type GroupClient struct{}

// FetchGroupIDsByDate 固定返回 7 个测试群聊 ID，忽略 date 参数。
func (c *GroupClient) FetchGroupIDsByDate(_ context.Context, date string) ([]int, error) {
	slog.Info("[Mock] FetchGroupIDsByDate", "date", date, "返回", []int{1, 2, 3, 4, 5, 6, 7})
	return []int{1, 2, 3, 4, 5, 6, 7}, nil
}

// FetchAllMessages 根据 groupID 返回对应场景的预置消息。
func (c *GroupClient) FetchAllMessages(_ context.Context, groupID int) ([]model.GroupMessage, error) {
	messages, ok := scenarioMessages[groupID]
	if !ok {
		return nil, fmt.Errorf("[Mock] 未找到 group_id=%d 的测试数据", groupID)
	}
	slog.Info("[Mock] FetchAllMessages", "group_id", groupID, "场景", scenarioDesc[groupID], "消息数", len(messages))
	return messages, nil
}

// ─────────────────────────────────────────────
//  ComplaintClient（实现 client.ComplaintSubmitter）
// ─────────────────────────────────────────────

// ComplaintClient 将提交结果打印到日志，不发起任何 HTTP 请求。
type ComplaintClient struct{}

// Submit 打印投诉/表扬详情和完整 JSON Body，模拟提交成功。
func (c *ComplaintClient) Submit(_ context.Context, groupID int, req model.ComplaintReq) error {
	typeStr := "正向激励(表扬/经验)"
	if req.Type == model.ComplaintWarning {
		typeStr = "改进提醒(警告)"
	}
	sourceStr := map[model.ComplaintSource]string{
		model.SourceAIUser:    "ai-用户",
		model.SourceAISystem:  "ai-系统",
		model.SourceAIService: "ai-服务人员",
	}[req.Source]

	body, _ := json.Marshal(req)
	slog.Info("[Mock] 提交投诉/表扬",
		"group_id", groupID,
		"type", typeStr,
		"source", sourceStr,
		"service_user_id", req.ServiceUserID,
		"apply_user_id", req.ApplyUserID,
		"body", string(body),
	)
	return nil
}

// ─────────────────────────────────────────────
//  测试场景数据
// ─────────────────────────────────────────────

var scenarioDesc = map[int]string{
	1: "用户问题被有效解决 → 期望生成[经验]",
	2: "用户明确表扬 → 期望生成[表扬]+[经验]",
	3: "服务人员未解决问题 → 期望生成[警告]",
	4: "服务人员响应超时 → 期望生成[响应超时警告]",
	5: "全天无服务人员消息 → 期望生成[零消息警告]",
	6: "制造焦虑+治病话术 → 期望生成[红线违规-三级:条款3]+[红线违规-一级:条款5f]",
	7: "强制指令+擅自要求停药 → 期望生成[红线违规-三级:条款6h]+[红线违规-一级:条款7o]",
}

// 基准时间，方便构造相对时间偏移
func ts(base time.Time, offsetMin int) string {
	return base.Add(time.Duration(offsetMin) * time.Minute).Format("2006-01-02 15:04:05")
}

var scenarioMessages = func() map[int][]model.GroupMessage {
	base := time.Date(2026, 6, 7, 9, 0, 0, 0, time.Local)

	return map[int][]model.GroupMessage{
		// ── 场景1：问题被有效解决 ─────────────────────────────
		// 期望：定性分析 → resolved=true → [经验]
		1: {
			{UserID: 101, UserName: "张伟", ContentType: "TEXT", Content: "你好，我的订单一直显示待支付，已经过了2个小时了", Role: 0, CreatedTime: ts(base, 0)},
			{UserID: 201, UserName: "李服务", ContentType: "TEXT", Content: "您好张伟，我马上帮您查一下", Role: 3, CreatedTime: ts(base, 3)},
			{UserID: 201, UserName: "李服务", ContentType: "TEXT", Content: "已查明，您的订单支付通道超时，我已帮您手动确认支付，请刷新页面查看", Role: 3, CreatedTime: ts(base, 5)},
			{UserID: 101, UserName: "张伟", ContentType: "TEXT", Content: "好了，谢谢！", Role: 0, CreatedTime: ts(base, 7)},
			{UserID: 202, UserName: "王辅助", ContentType: "TEXT", Content: "如有其他问题请随时联系我们", Role: 2, CreatedTime: ts(base, 8)},
		},

		// ── 场景2：用户明确表扬 ───────────────────────────────
		// 期望：defined性分析 → praised=true, resolved=true → [表扬] + [经验]
		2: {
			{UserID: 102, UserName: "陈晓", ContentType: "TEXT", Content: "请问如何修改收货地址？", Role: 0, CreatedTime: ts(base, 0)},
			{UserID: 203, UserName: "刘服务", ContentType: "TEXT", Content: "您好，请进入【我的订单】→【订单详情】→【修改地址】即可", Role: 3, CreatedTime: ts(base, 2)},
			{UserID: 102, UserName: "陈晓", ContentType: "TEXT", Content: "改好了！你们的服务太棒了，回答得非常快！👍👍👍", Role: 0, CreatedTime: ts(base, 4)},
			{UserID: 203, UserName: "刘服务", ContentType: "TEXT", Content: "感谢您的认可，祝您购物愉快！", Role: 3, CreatedTime: ts(base, 5)},
		},

		// ── 场景3：服务人员未解决问题 ─────────────────────────
		// 期望：定性分析 → resolved=false → [警告]
		3: {
			{UserID: 103, UserName: "赵明", ContentType: "TEXT", Content: "我的优惠券无法使用，提示「活动已结束」", Role: 0, CreatedTime: ts(base, 0)},
			{UserID: 204, UserName: "孙服务", ContentType: "TEXT", Content: "您好，优惠券有使用期限，请确认是否已过期", Role: 3, CreatedTime: ts(base, 3)},
			{UserID: 103, UserName: "赵明", ContentType: "TEXT", Content: "没有过期，有效期到6月30日", Role: 0, CreatedTime: ts(base, 5)},
			{UserID: 204, UserName: "孙服务", ContentType: "TEXT", Content: "那可能是系统问题，您可以稍后再试", Role: 3, CreatedTime: ts(base, 7)},
			{UserID: 103, UserName: "赵明", ContentType: "TEXT", Content: "一直不行，这问题没解决啊", Role: 0, CreatedTime: ts(base, 15)},
		},

		// ── 场景4：服务人员响应超时（>5分钟） ────────────────
		// 期望：定量分析 → 响应延迟 12 分钟 → [响应超时警告]
		4: {
			{UserID: 104, UserName: "吴静", ContentType: "TEXT", Content: "请问我的包裹今天能到吗？运单号是SF123456", Role: 0, CreatedTime: ts(base, 0)},
			// 故意间隔 12 分钟才回复
			{UserID: 205, UserName: "周服务", ContentType: "TEXT", Content: "您好，已帮您查询，预计今天18:00前送达", Role: 3, CreatedTime: ts(base, 12)},
			{UserID: 104, UserName: "吴静", ContentType: "TEXT", Content: "好的谢谢", Role: 0, CreatedTime: ts(base, 13)},
		},

		// ── 场景5：全天服务人员零消息 ─────────────────────────
		// 期望：定量分析 → totalServiceMsg=0 → [零消息警告]
		5: {
			{UserID: 105, UserName: "郑阳", ContentType: "TEXT", Content: "有人吗？我想咨询退款流程", Role: 0, CreatedTime: ts(base, 0)},
			{UserID: 105, UserName: "郑阳", ContentType: "TEXT", Content: "？？？", Role: 0, CreatedTime: ts(base, 10)},
			{UserID: 105, UserName: "郑阳", ContentType: "TEXT", Content: "客服都不在线吗", Role: 0, CreatedTime: ts(base, 20)},
		},

		// ── 场景6：制造焦虑 + 治病话术（红线违规） ───────────
		// 期望：
		//   红线违规-三级[条款3]  → 使用"数据很危险"制造焦虑
		//   红线违规-一级[条款5f] → 使用"治疗""根治"等治病话术
		6: {
			{UserID: 106, UserName: "林芳", ContentType: "TEXT", Content: "教练，我血糖最近有点偏高，怎么办？", Role: 0, CreatedTime: ts(base, 0)},
			{UserID: 206, UserName: "蔡教练", ContentType: "TEXT", Content: "林芳姐，你这数据很危险，再不调理就会越来越严重的，身体会出大问题！", Role: 1, CreatedTime: ts(base, 2)},
			{UserID: 106, UserName: "林芳", ContentType: "TEXT", Content: "那怎么调理呢？", Role: 0, CreatedTime: ts(base, 4)},
			{UserID: 206, UserName: "蔡教练", ContentType: "TEXT", Content: "坚持吃我们的产品，可以治疗糖尿病，根治血糖问题，比药物效果更好！", Role: 1, CreatedTime: ts(base, 5)},
			{UserID: 106, UserName: "林芳", ContentType: "TEXT", Content: "真的能治好吗？", Role: 0, CreatedTime: ts(base, 7)},
			{UserID: 206, UserName: "蔡教练", ContentType: "TEXT", Content: "肯定的！我们已经有很多客户通过这个方案把血糖调理好了。", Role: 1, CreatedTime: ts(base, 8)},
		},

		// ── 场景7：强制指令 + 擅自要求停药（红线违规） ──────
		// 期望：
		//   红线违规-三级[条款6h] → 使用"你必须""不准吃"等强制指令
		//   红线违规-一级[条款7o] → 主动要求用户停药
		7: {
			{UserID: 107, UserName: "孔亮", ContentType: "TEXT", Content: "教练，我最近工作很忙，没时间运动，饮食也没按餐单吃", Role: 0, CreatedTime: ts(base, 0)},
			{UserID: 207, UserName: "徐教练", ContentType: "TEXT", Content: "孔亮，你必须每天走够10000步，不准吃外卖，这是基本要求，必须做到！", Role: 1, CreatedTime: ts(base, 2)},
			{UserID: 107, UserName: "孔亮", ContentType: "TEXT", Content: "我真的很难做到，我还在吃降压药，有没有影响？", Role: 0, CreatedTime: ts(base, 5)},
			{UserID: 207, UserName: "徐教练", ContentType: "TEXT", Content: "降压药先停掉，我们的调理方案比药管用，长期吃药对身体不好的。", Role: 1, CreatedTime: ts(base, 7)},
			{UserID: 107, UserName: "孔亮", ContentType: "TEXT", Content: "停药没事吧？", Role: 0, CreatedTime: ts(base, 9)},
			{UserID: 207, UserName: "徐教练", ContentType: "TEXT", Content: "没事的，你先停一周试试，坚持执行我的方案。", Role: 1, CreatedTime: ts(base, 10)},
		},
	}
}()
