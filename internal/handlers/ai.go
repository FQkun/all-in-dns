package handlers

import (
	"net/http"

	"github.com/loeia/all-in-dns/internal/ai"
	"github.com/loeia/all-in-dns/internal/httpapi"
)

const aiSystemPrompt = `你是「DNS 统一管理平台」的诊断助手，服务对象是不懂 DNS 的建站小白和普通用户。

职责：
1. 用大白话解释 DNS 同步/解析问题，尽量不用术语；必须用术语时顺带一句解释或打比方
2. 给出具体、可操作的修复步骤（去哪个平台、点什么按钮）
3. 你可以调用工具查询用户平台的数据（域名列表、域名状态、同步日志、解析记录）来定位问题

常见错误含义参考：
- DomainRecordDuplicate：云平台上已存在「同主机+同线路+同类型」的记录，需要先删旧的
- DomainNotExists / InvalidParameterValue.DomainNotExists：域名还没在该云平台添加托管
- InvalidAccessKeyId / AuthFailure.SecretIdNotFound：AK/SK 密钥无效或填错
- InvalidDomainName.NoExist / DomainRecord.NotExist：域名不在该账号下
- 「待清理 / 远端删除失败」：云端删除没成功，定时任务会继续重试

输出要求：
- 先一句话说结论，再解释原因，最后给步骤
- 回答用中文、简洁、不要客套话
- 不确定时明确说「需要进一步确认」，绝不编造
- 你只能查询数据，不能修改任何配置；涉及改动时给出操作指引让用户自己做

如果用户问的问题与 DNS/本平台无关，礼貌说明你只能协助 DNS 相关问题。`

// aiChat AI 诊断对话（只读，通过工具查询数据）。
func (h *Handler) aiChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := h.auth.RequireUser(ctx, w, r); !ok {
		return
	}
	if !h.cfg.AIEnabled() || h.aiClient == nil || h.aiExec == nil {
		httpapi.Fail(w, http.StatusServiceUnavailable, "AI 诊断助手未配置（需设置 DNS_AI_API_KEY）")
		return
	}

	in := httpapi.Input(r)
	message := str(in["message"])
	if message == "" {
		httpapi.Fail(w, http.StatusBadRequest, "问题不能为空")
		return
	}

	// 上下文：当前页面 / 正在查看的域名
	messages := []ai.Message{{Role: "system", Content: aiSystemPrompt}}
	var contextInfo string
	if d := str(in["domain"]); d != "" {
		contextInfo = "用户当前正在查看域名：" + d
	}
	if p := str(in["page"]); p != "" {
		if contextInfo != "" {
			contextInfo += "\n"
		}
		contextInfo += "用户当前所在页面：" + p
	}
	if contextInfo != "" {
		messages = append(messages, ai.Message{Role: "system", Content: "上下文：\n" + contextInfo})
	}
	messages = append(messages, ai.Message{Role: "user", Content: message})

	tools := h.aiExec.Tools()

	// 工具调用循环（最多 5 轮，防止死循环）
	for i := 0; i < 5; i++ {
		resp, err := h.aiClient.Chat(ctx, messages, tools)
		if err != nil {
			httpapi.Fail(w, http.StatusBadGateway, err.Error())
			return
		}
		if len(resp.ToolCalls) == 0 {
			httpapi.OK(w, map[string]any{"answer": resp.Content})
			return
		}

		messages = append(messages, ai.Message{Role: "assistant", ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			result, err := h.aiExec.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				result = `{"error":"执行工具失败"}`
			}
			messages = append(messages, ai.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}

	httpapi.Fail(w, http.StatusBadGateway, "诊断轮次过多，请把问题描述得更具体一些")
}
