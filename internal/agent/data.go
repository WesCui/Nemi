package agent

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"nemi/internal/store"
)

func dataTools(s *state, st *store.Store) []tool.BaseTool {
	out := []tool.BaseTool{}
	add := func(name, desc string, params map[string]*schema.ParameterInfo, run func(context.Context, string) (any, error)) {
		out = append(out, &agentTool{info: &schema.ToolInfo{Name: name, Desc: desc, ParamsOneOf: schema.NewParamsOneOfByParams(params)}, s: s, run: run})
	}
	id := &schema.ParameterInfo{Type: schema.String, Required: true, Desc: "真实事项的 id"}
	revision := &schema.ParameterInfo{Type: schema.Integer, Required: true, Desc: "本次读取的真实版本；冲突时重新读取"}
	add("get_reminder", "读取真实事项的当前站内提醒、提醒版本、事项版本与截止时间。没有提醒时 reminder 为 null，expected_revision 为0。", map[string]*schema.ParameterInfo{"matter_id": id}, func(ctx context.Context, args string) (any, error) {
		var p struct {
			ID string `json:"matter_id"`
		}
		if parse(args, &p) != nil || len(p.ID) != 32 {
			return nil, errors.New("INVALID_ARGUMENTS")
		}
		return st.AgentReminder(ctx, s.ref.Workspace, p.ID)
	})
	add("propose_matter_update", "准备修改已有事项，用户确认后生效。expected_revision来自get_matter。可更新标题、资料、截止时间或ACTIVE/COMPLETED/ARCHIVED状态；归档保留历史并停用提醒，重新ACTIVE不会恢复旧提醒。清单用从0开始的索引修改/移除或追加，保留未选中的条目。", map[string]*schema.ParameterInfo{
		"matter_id": id, "expected_revision": revision, "title": {Type: schema.String}, "source": {Type: schema.String, Desc: "仅用户明确要求完整替换时填写；读取结果若截断，不得把摘录当作完整资料覆盖"}, "deadline": {Type: schema.String, Desc: "未来RFC3339时间"}, "clear_deadline": {Type: schema.Boolean}, "status": {Type: schema.String, Enum: []string{"ACTIVE", "COMPLETED", "ARCHIVED"}},
		"checklist_changes": {Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{"index": {Type: schema.Integer, Required: true}, "done": {Type: schema.Boolean}, "text": {Type: schema.String}, "remove": {Type: schema.Boolean}}}},
		"append_items":      {Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{"text": {Type: schema.String, Required: true}, "done": {Type: schema.Boolean}}}},
	}, func(ctx context.Context, args string) (any, error) {
		var p store.MatterChange
		if parse(args, &p) != nil {
			return nil, errors.New("INVALID_ARGUMENTS")
		}
		return st.ProposeMatterChange(ctx, s.ref, p)
	})
	add("propose_reminder_update", "准备调整或停用站内提醒，确认后生效。先get_reminder读取两个版本；enabled=true时提供已核对日期、频率、免打扰和周期结束日期，未提供的设置保留。enabled=false仅停用，保留原日期、频率、免打扰和结束日期。不得将站内提醒声称为手机或群通知。", map[string]*schema.ParameterInfo{
		"matter_id": id, "matter_revision": revision, "expected_revision": revision, "at": {Type: schema.String}, "enabled": {Type: schema.Boolean, Required: true}, "quiet": {Type: schema.Boolean}, "repeat": {Type: schema.String, Enum: []string{"once", "daily", "weekdays", "weekly"}}, "repeat_until": {Type: schema.String},
	}, func(ctx context.Context, args string) (any, error) {
		var p store.ReminderChange
		if parse(args, &p) != nil {
			return nil, errors.New("INVALID_ARGUMENTS")
		}
		return st.ProposeReminderChange(ctx, s.ref, p)
	})
	add("propose_memory", "根据用户明确要求保存/修改/移除偏好，等待用户确认，不能自动记忆。新建memory_id省略、expected_revision=0；修改/移除先list_memories获取id与版本。delete只移除偏好记录，聊天原文保留。", map[string]*schema.ParameterInfo{
		"operation": {Type: schema.String, Required: true, Enum: []string{"save", "delete"}}, "memory_id": {Type: schema.String}, "expected_revision": revision, "category": {Type: schema.String, Enum: []string{"general", "life", "travel", "work"}}, "text": {Type: schema.String},
	}, func(ctx context.Context, args string) (any, error) {
		var p store.MemoryChange
		if parse(args, &p) != nil {
			return nil, errors.New("INVALID_ARGUMENTS")
		}
		return st.ProposeMemoryChange(ctx, s.ref, p)
	})
	return out
}
