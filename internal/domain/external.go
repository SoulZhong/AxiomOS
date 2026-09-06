package domain

import (
	"strings"

	"github.com/teemo/axiomos/internal/i18n"
)

// 外部事件触发流程迁移（ADR 0020）。代码平台上真实发生的事（PR 打开、合并、CI 成败）到达系统时，
// 内核在当前状态里找一条声明了 `triggered_by` 且事件对得上的步骤：其余前提都满足就走它，
// 执行者是「外部事件」（不是人也不是 Agent，不为谁开执行记录）；否则什么都不改，只写一条说明动态。
// 组织在流程定义里配置哪一步由哪个事件触发（可定制）；触发时的状态类型语义写死（ADR 0014 的边界）。

// 外部事件来源。目前只有代码平台。
const SourceGit = "git"

// 六种外部事件。
const (
	EventPROpened = "pr_opened" // PR 打开
	EventPRReady  = "pr_ready"  // PR 转为可评审
	EventPRMerged = "pr_merged" // PR 合并
	EventPRClosed = "pr_closed" // PR 关闭（未合并）
	EventCIPassed = "ci_passed" // 检查通过
	EventCIFailed = "ci_failed" // 检查失败
)

// ExternalEvents 是全部外部事件，顺序即界面顺序。
var ExternalEvents = []string{EventPROpened, EventPRReady, EventPRMerged, EventPRClosed, EventCIPassed, EventCIFailed}

// ExternalEventTitle 是外部事件的显示名。
var ExternalEventTitle = map[string]i18n.Text{
	EventPROpened: i18n.T("PR 打开", "Pull request opened"),
	EventPRReady:  i18n.T("PR 可评审", "Pull request ready for review"),
	EventPRMerged: i18n.T("PR 合并", "Pull request merged"),
	EventPRClosed: i18n.T("PR 关闭", "Pull request closed"),
	EventCIPassed: i18n.T("检查通过", "Checks passed"),
	EventCIFailed: i18n.T("检查失败", "Checks failed"),
}

// BlockingEvents 是"坏消息"类外部事件：它们把任务推进等待中的状态时，系统给负责人一条「任务被阻塞」。
var BlockingEvents = []string{EventCIFailed}

// IsBlockingEvent 判断是不是坏消息类事件。
func IsBlockingEvent(e string) bool { return contains(BlockingEvents, e) }

// IsExternalEvent 判断是不是认识的外部事件。
func IsExternalEvent(e string) bool { return contains(ExternalEvents, e) }

// ExternalSources 是全部外部事件来源。
var ExternalSources = []string{SourceGit}

// IsExternalSource 判断是不是认识的来源。
func IsExternalSource(s string) bool { return contains(ExternalSources, s) }

// ExternalTrigger 是一件到达的外部事件，由应用层从 webhook 载荷翻译过来。
type ExternalTrigger struct {
	Source   string // git
	Event    string // pr_merged…
	Provider string // github | gitlab | gitee，只用来写句子
	// UserName 是外部操作者的登录名；MemberID 是它绑定到的本系统成员（没绑定时为空）。
	UserName string
	MemberID string
	// Ref 是这件事说的对象，写进句子里，如「PR #12」。
	Ref string
	URL string
}

// externalActor 造一个「外部事件」执行者：绑定了成员时用成员的编号（动态显示成那个人），否则没有编号。
func (tg ExternalTrigger) externalActor() *Executor {
	name := tg.UserName
	if name == "" {
		name = tg.Provider
	}
	return &Executor{ID: tg.MemberID, Kind: ExecutorExternal, Name: name}
}

// externalData 是要写进动态里的来源与外部操作者。
func (tg ExternalTrigger) externalData() map[string]any {
	return map[string]any{
		"external_source":    tg.Provider,
		"external_event":     tg.Event,
		"external_user_name": tg.UserName,
		"external_ref":       tg.Ref,
		"external_url":       tg.URL,
	}
}

// ExternalMatch 是一次外部事件在当前状态下的匹配结果。
type ExternalMatch struct {
	Transition Transition
	Found      bool
	Reasons    []Reason // 找到了但前提不满足时的原因
}

// MatchExternal 在当前状态里找由这件外部事件触发的步骤，并检查它的其余前提。
// 不检查 `by`（外部事件不是人也不是 Agent）与授权（授权是 Agent 的概念）。
func MatchExternal(c *Context, tg ExternalTrigger) ExternalMatch {
	t, wf := c.Task, &c.Type.Workflow
	cur := wf.State(t.State)
	for _, tr := range wf.Transitions {
		if tr.TriggeredBy == nil || tr.TriggeredBy.Source != tg.Source || tr.TriggeredBy.Event != tg.Event {
			continue
		}
		applies := false
		for _, f := range tr.From {
			if f == "*" {
				applies = applies || cur != nil && !cur.Label.IsTerminal()
			} else if f == t.State {
				applies = true
			}
		}
		if !applies {
			continue
		}
		var reasons []Reason
		for _, cond := range tr.Requires {
			if r := c.checkCondition(cond, Payload{}); r != nil {
				reasons = append(reasons, *r)
			}
		}
		if tr.To == "$previous" && t.PreviousState == "" {
			reasons = append(reasons, i18n.M("reject.no_previous"))
		}
		return ExternalMatch{Transition: tr, Found: true, Reasons: reasons}
	}
	return ExternalMatch{}
}

// ApplyExternal 让一件外部事件驱动流程：能走就走那一步（动态 ExternalEventApplied + 一条正常的状态变化），
// 不能走就只写一条 ExternalEventIgnored，任务一个字都不改。永远不返回错误——外部事件不该把 webhook 打挂。
func ApplyExternal(c *Context, tg ExternalTrigger) *Outcome {
	t := c.Task
	actor := tg.externalActor()
	ignore := func(rs ...Reason) *Outcome {
		o := &Outcome{Task: t}
		data := tg.externalData()
		data["reason"] = JoinReasons(rs)
		o.emit(c, "ExternalEventIgnored", actor.ID, data)
		return o
	}
	cur := c.Type.Workflow.State(t.State)
	if cur == nil || cur.Label.IsTerminal() {
		return ignore(i18n.M("reject.task_closed"))
	}
	m := MatchExternal(c, tg)
	if !m.Found {
		return ignore(i18n.M("external.no_transition", ExternalEventTitle[tg.Event], cur.Title))
	}
	if len(m.Reasons) > 0 {
		return ignore(m.Reasons...)
	}
	o, err := applyStep(c, actor, m.Transition, Payload{})
	if err != nil {
		if rj, ok := err.(*Rejection); ok && len(rj.Reasons) > 0 {
			return ignore(rj.Reasons...)
		}
		return ignore(i18n.M("external.internal", err.Error()))
	}
	// 头条动态：外部事件驱动了这一步。状态变化本身仍走 TaskTransitioned（统计与燃尽图靠它回放）。
	data := tg.externalData()
	data["transition"] = m.Transition.Name
	if st := c.Type.Workflow.State(t.State); st != nil {
		data["to"] = st.Name
		data["to_title"] = st.Title
	}
	applied := Event{Type: "ExternalEventApplied", TaskID: t.ID, ActorID: actor.ID, At: c.now(), Data: data}
	o.Events = append([]Event{applied}, o.Events...)
	return o
}

// JoinReasons 把若干条原因渲染成一段各语言都有的文本（分号连接），好放进动态数据里。
func JoinReasons(rs []Reason) i18n.Text {
	out := i18n.Text{}
	for _, l := range i18n.Supported {
		parts := make([]string, 0, len(rs))
		for _, r := range rs {
			parts = append(parts, r.Render(l))
		}
		sep := "；"
		if l == i18n.EnUS {
			sep = "; "
		}
		out[l] = strings.Join(parts, sep)
	}
	return out
}
