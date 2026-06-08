package client

import (
	"context"

	"group-audit/internal/model"
)

// GroupFetcher 群聊数据拉取接口。
// 真实实现：GroupClient；测试实现：mock.GroupClient。
type GroupFetcher interface {
	FetchGroupIDsByDate(ctx context.Context, date string) ([]int, error)
	FetchAllMessages(ctx context.Context, groupID int) ([]model.GroupMessage, error)
}

// ComplaintSubmitter 投诉/表扬提交接口。
// 真实实现：ComplaintClient；测试实现：mock.ComplaintClient。
type ComplaintSubmitter interface {
	Submit(ctx context.Context, groupID int, req model.ComplaintReq) error
}
