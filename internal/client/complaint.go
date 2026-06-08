package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"group-audit/internal/model"

	"github.com/cenkalti/backoff/v4"
)

// ComplaintClient 封装对投诉/表扬接口的调用。
type ComplaintClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewComplaintClient 创建 ComplaintClient。
func NewComplaintClient(baseURL string) *ComplaintClient {
	return &ComplaintClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Submit 调用 POST /ai/complaint/groups/:id 提交单条投诉/表扬，带指数退避重试。
func (c *ComplaintClient) Submit(ctx context.Context, groupID int, req model.ComplaintReq) error {
	// 确保 Pic 不为 nil（后端可能校验必填）
	if req.Pic == nil {
		req.Pic = []string{}
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化投诉请求体失败: %w", err)
	}

	slog.Info("提交投诉", "group_id", groupID, "body", string(bodyBytes))

	op := func() error {
		url := fmt.Sprintf("%s/ai/complaint/groups/%d", c.baseURL, groupID)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return backoff.Permanent(err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			return fmt.Errorf("提交投诉服务端错误 %d，将重试", resp.StatusCode)
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return backoff.Permanent(fmt.Errorf("提交投诉返回 %d", resp.StatusCode))
		}
		return nil
	}

	bo := newDefaultBackOff()
	return backoff.Retry(op, backoff.WithContext(bo, ctx))
}
