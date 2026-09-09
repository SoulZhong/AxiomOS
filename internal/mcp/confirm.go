package mcp

import (
	"context"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/i18n"
)

// 在 Agent 里确认（ADR 0027）：斜杠命令 /confirm 只有人能敲——客户端在人敲的那一刻向服务器取提示
// （prompts/get），服务器趁这一步发一张十分钟有效、只覆盖人点名的那几条待确认操作的凭证，写进提示文本；
// Agent 再拿凭证调 decide_proposal，服务器以所有者本人的身份裁决。Agent 自己调不到 prompts/get，
// 所以拿不到凭证；凭证也只对点名的那几条有效，Agent 做不了人没说的事。

type decideIn struct {
	ProposalID string   `json:"proposal_id" jsonschema:"Pending action ID (prp_...) listed in the /confirm prompt"`
	Decision   string   `json:"decision" jsonschema:"approve or reject"`
	Nonce      string   `json:"nonce" jsonschema:"The confirmation ticket from the /confirm prompt; valid 10 minutes, only for the listed actions"`
	Reason     string   `json:"reason,omitempty" jsonschema:"Required when rejecting: one full sentence the agent will read"`
	Skip       []string `json:"skip,omitempty" jsonschema:"Goal plan only: keys to leave out"`
	AssigneeID string   `json:"assignee_id,omitempty" jsonschema:"Goal plan only: reassign every task to this person (@name, ID or email)"`
}

var confirmDescs = map[string]i18n.Text{
	"decide_proposal": i18n.T("以所有者本人的身份确认或拒绝一条待确认操作。只在人敲了 /confirm 斜杠命令、提示里给了凭证（nonce）之后才能用；凭证十分钟有效，只对提示里列出的那几条有效，每条一次。没有凭证不要调，也不要替人做决定。",
		"Confirm or reject a pending action on behalf of the owner. Usable only after the person ran the /confirm slash command and the prompt gave you a ticket (nonce); the ticket lasts 10 minutes, covers only the actions listed in the prompt, once each. Never call it without a ticket, and never decide for the person."),
}

func init() {
	for k, v := range confirmDescs {
		toolDescs[k] = v
	}
}

var confirmPrompt = promptSpec{
	Name:  "confirm",
	Title: i18n.T("确认待确认操作", "Confirm pending actions"),
	Desc:  i18n.T("不离开客户端就把等你确认的操作确认或拒绝掉：不填 which 只列清单；which=all 全部，或填一条的 ID。", "Confirm or reject the actions waiting for you without leaving the client: leave which empty to list them; which=all for all of them, or one action's ID."),
	Args: []promptArg{
		{Name: "which", Title: i18n.T("哪些", "Which"), Desc: i18n.T("all（全部等我确认的）或一条待确认操作的 ID；不填只列清单。", "all (everything awaiting me) or one pending action's ID; empty lists them.")},
		{Name: "decision", Title: i18n.T("决定", "Decision"), Desc: i18n.T("approve（默认）或 reject。", "approve (default) or reject.")},
		{Name: "reason", Title: i18n.T("拒绝理由", "Reason"), Desc: i18n.T("拒绝时必填：一句完整的话，Agent 会读它。", "Required when rejecting: one full sentence the agent will read.")},
	},
}

// addConfirm 注册 /confirm 提示与 decide_proposal 工具。
func addConfirm(s *sdk.Server, k *kit) {
	a, sess, loc := k.a, k.sess, k.loc
	p := &sdk.Prompt{Name: confirmPrompt.Name, Title: confirmPrompt.Title.In(loc), Description: confirmPrompt.Desc.In(loc)}
	for _, ar := range confirmPrompt.Args {
		p.Arguments = append(p.Arguments, &sdk.PromptArgument{Name: ar.Name, Title: ar.Title.In(loc), Description: ar.Desc.In(loc)})
	}
	s.AddPrompt(p, func(ctx context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		in := parsePromptInput(confirmPrompt.Args, req)
		return &sdk.GetPromptResult{Description: p.Title, Messages: []*sdk.PromptMessage{{Role: sdk.Role("user"), Content: &sdk.TextContent{Text: confirmText(ctx, a, sess, in)}}}}, nil
	})

	sdk.AddTool(s, k.tool("decide_proposal"), func(ctx context.Context, req *sdk.CallToolRequest, in decideIn) (*sdk.CallToolResult, any, error) {
		opts := app.ApproveOptions{Skip: in.Skip}
		if in.AssigneeID != "" {
			who, err := k.mid(ctx, in.AssigneeID)
			if err != nil {
				return k.f(err)
			}
			opts.AssigneeID = who
		}
		out, err := a.DecideViaAgent(ctx, sess, in.Nonce, in.ProposalID, in.Decision, in.Reason, opts)
		if err != nil {
			return k.f(err)
		}
		return jsonResult(map[string]any{"ok": true, "proposal_id": in.ProposalID, "decision": strings.ToLower(strings.TrimSpace(in.Decision)), "result": out})
	})
}

// confirmText 生成 /confirm 的指令：列出范围里的待确认操作，带上凭证，点名要调的工具。
func confirmText(ctx context.Context, a *app.App, sess *app.Session, in promptInput) string {
	loc := sess.Loc()
	which := strings.TrimSpace(in.get("which"))
	decision := strings.ToLower(strings.TrimSpace(in.get("decision")))
	if decision == "" {
		decision = "approve"
	}
	reason := in.get("reason")
	if !sess.IsAgent() {
		return tr(loc, "这条命令只在 Agent 客户端里用；你本人直接在网页的「待我处理」里确认。", "This command is for use inside an agent client; you can confirm directly in the web inbox.")
	}
	// 没点名：只列清单，不发凭证
	if which == "" {
		list, err := a.ListProposalsForOwner(ctx, sess)
		if err != nil {
			return RenderError(loc, err)
		}
		if len(list) == 0 {
			return tr(loc, "现在没有等人确认的待确认操作。告诉人一声就行。", "Nothing is awaiting confirmation right now. Just tell the person.")
		}
		lines := []string{tr(loc, "下面是等人确认的待确认操作。把它们念给人听（编号、要做什么、针对谁），然后停下：人要确认或拒绝时会再敲 /confirm 并带上 which=all 或某一条的 ID；没有那一步不要调 decide_proposal。",
			"Below are the actions awaiting the person's confirmation. Read them out (ID, what it does, on what), then stop: to confirm or reject, the person runs /confirm again with which=all or one action's ID; without that step do not call decide_proposal.")}
		for _, p := range list {
			lines = append(lines, proposalLine(p))
		}
		return joinLines(lines...)
	}
	if decision == "reject" && strings.TrimSpace(reason) == "" {
		return tr(loc, "拒绝要写理由：请人再敲一次 /confirm，带上 reason（一句完整的话，Agent 会读它）。没有理由不要调 decide_proposal。",
			"Rejecting needs a reason: ask the person to run /confirm again with reason (one full sentence the agent will read). Do not call decide_proposal without it.")
	}
	ticket, err := a.MintConfirmTicket(ctx, sess, app.ConfirmScope{Which: which, Decision: decision})
	if err != nil {
		return RenderError(loc, err)
	}
	verb := tr(loc, "确认", "confirm")
	if decision == "reject" {
		verb = tr(loc, "拒绝", "reject")
	}
	lines := []string{
		tr(loc, "人刚刚在客户端里敲了 /confirm，要"+verb+"下面这些待确认操作。这是人本人的决定，你只是转达：",
			"The person just ran /confirm in the client to "+verb+" the pending actions below. This is the person's own decision; you are only relaying it:"),
	}
	for _, p := range ticket.Proposals {
		lines = append(lines, proposalLine(p))
	}
	lines = append(lines,
		tr(loc, "对上面每一条各调用一次 decide_proposal：proposal_id 写那条的 ID，decision=\""+decision+"\"，nonce=\""+ticket.Nonce+"\"。",
			"Call decide_proposal once per line above: proposal_id is that line's ID, decision=\""+decision+"\", nonce=\""+ticket.Nonce+"\"."),
	)
	if decision == "reject" {
		lines = append(lines, quoteBlock(loc, tr(loc, "reason 用下面这段原文", "Use the text below verbatim as reason"), reason))
	}
	lines = append(lines,
		tr(loc, "凭证十分钟内有效，只对上面列出的这几条有效，每条只能用一次；人没点名的不要碰，凭证失效就请人重新敲 /confirm。",
			"The ticket lasts 10 minutes, covers only the lines above, once each; do not touch anything the person did not name, and if the ticket expires ask them to run /confirm again."),
		tr(loc, "目标方案（提交目标方案）要逐条勾选的话，人得在网页上做；在这里确认就是整份批准。",
			"A goal plan can only be reviewed item by item on the web; confirming here approves the whole plan."),
		reportLine(loc),
	)
	return joinLines(lines...)
}

// proposalLine 是清单里的一行：编号、动作、说明、针对谁。
func proposalLine(p *app.ProposalView) string {
	line := "- " + p.ID + "：" + p.ActionTitle + " · " + p.SummaryText
	if p.TargetTitle != "" {
		line += "（" + p.TargetTitle + "）"
	}
	return line
}
