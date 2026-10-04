package agent

import (
	"context"
	"errors"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"nemi/internal/store"
)

func continuationTool(s *state, st *store.Store) tool.BaseTool {
	return &agentTool{s: s, info: &schema.ToolInfo{Name: "await_actions", Desc: "本轮已准备操作，后续工作必须等待用户确认时，保存原始目标和具体下一步并暂停。本工具必须单独调用，不能和其他工具同批。action_ids为本轮真实提案ID，最多4个且至少一个待确认。用户需在卡片允许继续，不能自行授予权限。简单保存后结束的任务无需调用。", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"goal": {Type: schema.String, Required: true}, "next_step": {Type: schema.String, Required: true}, "action_ids": {Type: schema.Array, Required: true, ElemInfo: &schema.ParameterInfo{Type: schema.String}}})}, run: func(ctx context.Context, args string) (any, error) {
		var p store.WaitForActions
		if parse(args, &p) != nil {
			return nil, errors.New("INVALID_ARGUMENTS")
		}
		c, err := st.WaitForActions(ctx, s.ref, p)
		if err != nil {
			return nil, err
		}
		if err = react.SetReturnDirectly(ctx); err != nil {
			return nil, err
		}
		s.waiting = &c
		return map[string]string{"status": "WAITING_CONFIRMATION", "goal": c.Goal, "next_step": c.Next}, nil
	}}
}
