package app

import (
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 动态的句子（ADR 0002：动态是骨架）。HTTP 与 MCP 两个薄接口层都要把一条动态渲染成
// 「某人 做了什么」的一句话，所以渲染放在应用层，两边各自只做外形转换。names 是执行者 ID → 名字。

// textOf 把动态数据里的多语言字段（JSON 对象或字符串）解析成该语言文本。
func TextOf(v any, loc i18n.Locale) string {
	switch x := v.(type) {
	case string:
		return x
	case i18n.Text:
		return x.In(loc)
	case map[string]any:
		t := i18n.Text{}
		for k, val := range x {
			if s, ok := val.(string); ok {
				if l := i18n.Normalize(k); l != "" {
					t[l] = s
				}
			}
		}
		return t.In(loc)
	}
	return ""
}

// externalWho 把「系统」换成外部操作者（如「GitHub 用户 alice」），动态里带了来源与登录名时用。
func externalWho(who string, e *store.EventRow, loc i18n.Locale) string {
	src := TextOf(e.Data["external_source"], loc)
	name := TextOf(e.Data["external_user_name"], loc)
	if e.ActorID != "" || src == "" || name == "" {
		return who
	}
	return i18n.Trf(loc, "external.actor", SourceTitle(src, loc), name)
}

func EventSummary(e *store.EventRow, names map[string]string, taskTitle map[string]string, roles map[string]i18n.Text, loc i18n.Locale) string {
	who := i18n.Tr(loc, "ev.system")
	if n := names[e.ActorID]; n != "" {
		who = n
	}
	task := ""
	if t, ok := taskTitle[e.TaskID]; ok {
		task = "「" + t + "」"
		if loc == i18n.EnUS {
			task = "\"" + t + "\""
		}
	}
	s := func(k string) string { return TextOf(e.Data[k], loc) }
	switch e.Type {
	case "TaskCreated":
		return i18n.Trf(loc, "ev.TaskCreated", who, task)
	case "TaskAssigned":
		name := s("to")
		if n := names[name]; n != "" {
			name = n
		}
		if via := s("via_slot"); via != "" {
			return i18n.Trf(loc, "ev.TaskAssignedVia", task, via, name)
		}
		return i18n.Trf(loc, "ev.TaskAssigned", who, task, name)
	case "TaskClaimed":
		return i18n.Trf(loc, "ev.TaskClaimed", who, task)
	case "TaskSentToBacklog":
		if role := s("required_role"); role != "" {
			return i18n.Trf(loc, "ev.TaskSentToBacklogR", task, RoleTitle(roles, role, loc))
		}
		return i18n.Trf(loc, "ev.TaskSentToBacklog", task)
	case "TaskTransitioned":
		to := s("to_title")
		if to == "" {
			to = s("to")
		}
		return i18n.Trf(loc, "ev.TaskTransitioned", who, s("title"), task, to)
	case "RunStarted":
		return i18n.Trf(loc, "ev.RunStarted", who, task)
	case "RunEnded":
		return i18n.Trf(loc, "ev.RunEnded", who, task, i18n.Tr(loc, "outcome."+s("outcome")))
	case "UsageReported":
		return i18n.Trf(loc, "ev.UsageReported", who, task)
	case "ArtifactAttached":
		return i18n.Trf(loc, "ev.ArtifactAttached", who, task, s("title"))
	case "CommentAdded":
		return i18n.Trf(loc, "ev.CommentAdded", who, task, s("text"))
	case "NoteAdded":
		return i18n.Trf(loc, "ev.NoteAdded", who, task)
	case "TasksLinked":
		return i18n.Trf(loc, "ev.TasksLinked", who, task)
	case "RelationRemoved":
		return i18n.Trf(loc, "ev.RelationRemoved", task)
	case "GoalCreated":
		return i18n.Trf(loc, "ev.GoalCreated", who, s("title"))
	case "GoalUpdated":
		return i18n.Trf(loc, "ev.GoalUpdated", who)
	case "GoalFieldChanged":
		return fieldChangeSummary(e, names, loc, who, "goal", i18n.Trf(loc, "ev.obj.goal", s("title")))
	case "GoalNoteAdded":
		return i18n.Trf(loc, "ev.GoalNoteAdded", who, s("title"), s("text"))
	case "GoalPlanApplied":
		tc, _ := NumOf(e.Data["task_count"])
		mc, _ := NumOf(e.Data["milestone_count"])
		return i18n.Trf(loc, "ev.GoalPlanApplied", who, s("title"), tc, mc)
	case "MandateIssued":
		return i18n.Trf(loc, "ev.MandateIssued", who, task, nameOr(names, s("agent_id")))
	case "MandateRevoked":
		return i18n.Trf(loc, "ev.MandateRevoked", who, nameOr(names, s("agent_id")), task)
	case "MandateStale":
		return i18n.Trf(loc, "ev.MandateStale", nameOr(names, s("agent_id")), task, i18n.Tr(loc, s("reason")))
	case "TaskAutoAccepted":
		return i18n.Trf(loc, "ev.TaskAutoAccepted", task)
	case "TaskReverted":
		to := s("to_title")
		if to == "" {
			to = s("to")
		}
		return i18n.Trf(loc, "ev.TaskReverted", who, task, to, s("reason"))
	case "PlanReviewRequested":
		tc, _ := NumOf(e.Data["task_count"])
		hc, _ := NumOf(e.Data["human_count"])
		return i18n.Trf(loc, "ev.PlanReviewRequested", s("title"), tc, hc)
	case "PlanReviewed":
		ac, _ := NumOf(e.Data["accepted"])
		mc, _ := NumOf(e.Data["milestones"])
		sk, _ := NumOf(e.Data["skipped"])
		return i18n.Trf(loc, "ev.PlanReviewed", who, s("title"), ac, mc, sk)
	case "ProposalStale":
		return i18n.Trf(loc, "ev.ProposalStale", nameOr(names, s("agent_id")), ProposalSummaryText(s("summary")))
	case "TaskFieldChanged":
		return fieldChangeSummary(e, names, loc, who, "task", i18n.Trf(loc, "ev.obj.task", task))
	case "MilestoneCreated", "MilestoneUpdated", "MilestoneReached", "MilestoneUnreached", "MilestoneDeleted":
		var due any = s("due_on")
		if t, err := time.ParseInLocation("2006-01-02", s("due_on"), time.Local); err == nil {
			due = i18n.Date(t)
		}
		return i18n.Trf(loc, "ev."+e.Type, who, s("goal_title"), s("title"), due)
	case "AgentRegistered":
		return i18n.Trf(loc, "ev.AgentRegistered", who, s("name"))
	case "AgentConnected":
		return i18n.Trf(loc, "ev.AgentConnected", who, s("name"), i18n.Tr(loc, "device.client."+s("client")))
	case "PreferencesUpdated":
		cleared, _ := e.Data["cleared"].(bool)
		var fields []string
		if keys, ok := e.Data["fields"].([]any); ok {
			for _, k := range keys {
				fields = append(fields, i18n.Tr(loc, "pref.field."+TextOf(k, loc)))
			}
		}
		if s("target") == "role" {
			role := RoleTitle(roles, s("role"), loc)
			if cleared {
				return i18n.Trf(loc, "ev.PreferencesRoleCleared", who, role)
			}
			return i18n.Trf(loc, "ev.PreferencesRole", who, role, fields)
		}
		if cleared {
			return i18n.Trf(loc, "ev.PreferencesPersonalCleared", who)
		}
		return i18n.Trf(loc, "ev.PreferencesPersonal", who, fields)
	case "AgentRemoved", "AgentRevoked":
		return i18n.Trf(loc, "ev.AgentRemoved", who)
	case "AgentUpdated":
		return i18n.Trf(loc, "ev.AgentUpdated", who)
	case "TaskUpdated":
		return i18n.Trf(loc, "ev.TaskUpdated", who, task)
	case "TaskTypeSaved":
		return i18n.Trf(loc, "ev.TaskTypeSaved", who, s("name"))
	case "MemberInvited":
		return i18n.Trf(loc, "ev.MemberInvited", who, s("email"))
	case "InvitationRevoked":
		return i18n.Trf(loc, "ev.InvitationRevoked", who, s("email"))
	case "OrgSettingsUpdated":
		return i18n.Trf(loc, "ev.OrgSettingsUpdated", who, i18n.Tr(loc, "visibility."+s("collaboration_visibility")), i18n.Tr(loc, "visibility."+s("finance_visibility")))
	case "WorkspaceLayoutUpdated":
		cleared, _ := e.Data["cleared"].(bool)
		if s("target") != "role" {
			if cleared {
				return i18n.Trf(loc, "ev.WorkspacePersonalCleared", who)
			}
			return i18n.Trf(loc, "ev.WorkspacePersonal", who)
		}
		role := RoleTitle(roles, s("role"), loc)
		if cleared {
			return i18n.Trf(loc, "ev.WorkspaceRoleCleared", who, role)
		}
		if p, ok := domain.PresetByKey(s("preset")); ok {
			return i18n.Trf(loc, "ev.WorkspaceRolePreset", who, role, p.Title)
		}
		var titles []i18n.Text
		if keys, ok := e.Data["blocks"].([]any); ok {
			for _, k := range keys {
				if b, ok := domain.BlockByKey(TextOf(k, loc)); ok {
					titles = append(titles, b.Title)
				}
			}
		}
		return i18n.Trf(loc, "ev.WorkspaceRoleBlocks", who, role, titles)
	case "GoalTypeCreated", "GoalTypeUpdated", "GoalTypeDeactivated", "GoalTypeReactivated", "GoalTypeDeleted":
		// 目标类型（ADR 0023）：句子里念的是动态里记下的那个名字，之后改名不影响历史
		return i18n.Trf(loc, "ev."+e.Type, who, s("name"))
	case "GoalTypeRenamed":
		return i18n.Trf(loc, "ev.GoalTypeRenamed", who, s("from"), s("name"))
	case "TeamCreated":
		return i18n.Trf(loc, "ev.TeamCreated", who, s("name"))
	case "TeamUpdated":
		return i18n.Trf(loc, "ev.TeamUpdated", who, s("name"))
	case "TeamDeleted":
		return i18n.Trf(loc, "ev.TeamDeleted", who)
	case "TeamBoundaryChanged":
		if on, _ := e.Data["is_boundary"].(bool); on {
			return i18n.Trf(loc, "ev.TeamBoundaryOn", who, s("name"))
		}
		return i18n.Trf(loc, "ev.TeamBoundaryOff", who, s("name"))
	case "MemberJoined":
		return i18n.Trf(loc, "ev.MemberJoined", who)
	case "TeamDeactivated":
		if manual, _ := e.Data["manual"].(bool); manual {
			return i18n.Trf(loc, "ev.TeamDeactivatedManual", who, s("name"))
		}
		return i18n.Trf(loc, "ev.TeamDeactivated", who, s("name"))
	case "TeamMoved":
		if s("parent_id") == "" {
			return i18n.Trf(loc, "ev.TeamMovedTop", who, s("name"))
		}
		return i18n.Trf(loc, "ev.TeamMoved", who, s("name"), s("parent_name"))
	case "MemberSynced", "MemberUpdated", "MemberDeactivated", "MemberActivated", "MemberReactivated", "MemberImported", "MemberTeamCleared", "TeamReactivated":
		return i18n.Trf(loc, "ev."+e.Type, who, s("name"))
	case "MemberRenamed":
		return i18n.Trf(loc, "ev.MemberRenamed", who, s("from"), s("name"))
	case "MemberRolesChanged":
		var titles []string
		if names, ok := e.Data["roles"].([]any); ok {
			for _, n := range names {
				titles = append(titles, RoleTitle(roles, TextOf(n, loc), loc))
			}
		}
		if len(titles) == 0 {
			return i18n.Trf(loc, "ev.MemberRolesCleared", who, s("name"))
		}
		return i18n.Trf(loc, "ev.MemberRolesChanged", who, s("name"), titles)
	case "MemberTeamChanged":
		if s("mode") == "add" {
			return i18n.Trf(loc, "ev.MemberTeamAdded", who, s("name"), s("team_name"))
		}
		return i18n.Trf(loc, "ev.MemberTeamChanged", who, s("name"), s("team_name"))
	case "TeamMerged", "MemberMerged":
		return i18n.Trf(loc, "ev."+e.Type, who, s("name"), s("into_name"))
	case "DirectoryDecided":
		ext := s("external_name")
		if ext == "" {
			ext = s("external_id")
		}
		switch s("decision") {
		case "merge":
			return i18n.Trf(loc, "ev.DirectoryDecidedMerge", who, ext, s("local_name"))
		case "create":
			return i18n.Trf(loc, "ev.DirectoryDecidedCreate", who, ext)
		case "skip":
			return i18n.Trf(loc, "ev.DirectoryDecidedSkip", who, ext)
		}
		return i18n.Trf(loc, "ev.DirectoryDecidedReconsider", who, ext)
	case "DirectoryUnbound":
		return i18n.Trf(loc, "ev.DirectoryUnbound", who, s("name"))
	case "DirectoryConfigured":
		if names := FieldTitles(s("provider"), e.Data["fields"], loc); names != "" {
			return i18n.Trf(loc, "ev.DirectoryFieldsChanged", who, SourceTitle(s("provider"), loc), names)
		}
		return i18n.Trf(loc, "ev.DirectoryConfigured", who, SourceTitle(s("provider"), loc))
	// 代码平台与外部事件（ADR 0020）
	case "CodePlatformDisconnected":
		return i18n.Trf(loc, "ev.CodePlatformDisconnected", who, SourceTitle(s("provider"), loc))
	case "DirectoryDisconnected":
		return i18n.Trf(loc, "ev.DirectoryDisconnected", who, SourceTitle(s("provider"), loc))
	case "CalendarConfigured":
		fresh, _ := e.Data["fresh"].(bool)
		if names := FieldTitles(s("provider"), e.Data["fields"], loc); names != "" && !fresh {
			return i18n.Trf(loc, "ev.CalendarFieldsChanged", who, SourceTitle(s("provider"), loc), names)
		}
		return i18n.Trf(loc, "ev.CalendarConfigured", who, SourceTitle(s("provider"), loc))
	case "CalendarDisconnected":
		return i18n.Trf(loc, "ev.CalendarDisconnected", who, SourceTitle(s("provider"), loc))
	case "CalendarSyncRan":
		if s("status") != "ok" {
			return i18n.Trf(loc, "ev.CalendarSyncFailed", SourceTitle(s("provider"), loc), s("error"))
		}
		mc, _ := NumOf(e.Data["members"])
		ec, _ := NumOf(e.Data["events"])
		return i18n.Trf(loc, "ev.CalendarSyncRan", SourceTitle(s("provider"), loc), i18n.Tr(loc, "directory.status.ok"), mc, ec)
	case "CalendarIdentityBound":
		return i18n.Trf(loc, "ev.CalendarIdentityBound", who, SourceTitle(s("provider"), loc))
	case "CalendarIdentityRemoved":
		return i18n.Trf(loc, "ev.CalendarIdentityRemoved", who, SourceTitle(s("provider"), loc))
	case "CodePlatformConfigured":
		fresh, _ := e.Data["fresh"].(bool)
		switched, _ := e.Data["provider_switched"].(bool)
		if names := FieldTitles(s("provider"), e.Data["fields"], loc); names != "" && !fresh && !switched {
			return i18n.Trf(loc, "ev.CodePlatformFieldsChanged", who, SourceTitle(s("provider"), loc), names)
		}
		return i18n.Trf(loc, "ev.CodePlatformConfigured", who, SourceTitle(s("provider"), loc))
	case "CodeWebhookSecretRotated", "CodeWebhookSecretRevealed":
		return i18n.Trf(loc, "ev."+e.Type, who, SourceTitle(s("provider"), loc))
	case "CodeIdentityBound":
		if login := s("login"); login != "" {
			return i18n.Trf(loc, "ev.CodeIdentityBound", who, SourceTitle(s("provider"), loc), login)
		}
		return i18n.Trf(loc, "ev.CodeIdentityUnbound", who, SourceTitle(s("provider"), loc))
	case "ExternalLinkAdded":
		return i18n.Trf(loc, "ev.ExternalLinkAdded", externalWho(who, e, loc), task, s("kind_title"), s("title"))
	case "ExternalLinkUpdated":
		return i18n.Trf(loc, "ev.ExternalLinkUpdated", task, s("kind_title"), s("title"), s("status_title"))
	case "ExternalLinkRemoved":
		return i18n.Trf(loc, "ev.ExternalLinkRemoved", who, task, s("title"))
	case "ExternalEventApplied":
		return i18n.Trf(loc, "ev.ExternalEventApplied", SourceTitle(s("external_source"), loc), s("external_ref"),
			i18n.Tr(loc, "external.verb."+s("external_event")), task, s("to_title"))
	case "ExternalEventIgnored":
		return i18n.Trf(loc, "ev.ExternalEventIgnored", SourceTitle(s("external_source"), loc), s("external_ref"),
			i18n.Tr(loc, "external.verb."+s("external_event")), task, s("reason"))
	case "NotificationChannelConfigured":
		title := s("channel")
		if p, ok := directory.Lookup(s("channel")); ok {
			title = p.Title.In(loc)
		}
		if enabled, _ := e.Data["enabled"].(bool); !enabled {
			return i18n.Trf(loc, "ev.NotificationChannelDisabled", who, title)
		}
		return i18n.Trf(loc, "ev.NotificationChannelConfigured", who, title)
	case "NotificationPolicyChanged":
		var titles []i18n.Text
		if keys, ok := e.Data["allowed_kinds"].([]any); ok {
			for _, k := range keys {
				if t, ok := domain.NotifyKindTitle[TextOf(k, loc)]; ok {
					titles = append(titles, t)
				}
			}
		}
		if len(titles) == 0 {
			return i18n.Trf(loc, "ev.NotificationPolicyNone", who)
		}
		return i18n.Trf(loc, "ev.NotificationPolicyChanged", who, titles)
	case "NotificationPreferencesUpdated":
		if cleared, _ := e.Data["cleared"].(bool); cleared {
			return i18n.Trf(loc, "ev.NotificationPreferencesCleared", who)
		}
		return i18n.Trf(loc, "ev.NotificationPreferencesUpdated", who)
	case "DirectoryDetached":
		return i18n.Trf(loc, "ev.DirectoryDetached", who, s("name"), SourceTitle(s("old_provider"), loc))
	case "DirectorySyncRan":
		if s("status") == "failed" {
			return i18n.Trf(loc, "ev.DirectorySyncFailed", s("error"))
		}
		n := func(k string) int { v, _ := NumOf(e.Data[k]); return v }
		out := i18n.Trf(loc, "ev.DirectorySyncRan", n("added_teams"), n("added_members"), n("updated_teams"), n("updated_members"), n("deactivated_teams"), n("deactivated_members"))
		if k := n("errors"); k > 0 {
			out += i18n.Trf(loc, "ev.DirectorySyncErrors", k)
		}
		return out
	case "SprintCreated", "SprintUpdated", "SprintStarted":
		return i18n.Trf(loc, "ev."+e.Type, who, s("name"))
	case "SprintClosed":
		moved, _ := NumOf(e.Data["moved"])
		returned, _ := NumOf(e.Data["returned"])
		return i18n.Trf(loc, "ev.SprintClosed", who, s("name"), moved, returned)
	case "TaskAddedToSprint", "TaskRemovedFromSprint":
		return i18n.Trf(loc, "ev."+e.Type, who, task, s("name"))
	case "PointsChanged":
		if p, ok := NumOf(e.Data["points"]); ok {
			return i18n.Trf(loc, "ev.PointsChanged", who, task, p)
		}
		return i18n.Trf(loc, "ev.PointsCleared", who, task)
	case "ProposalCreated":
		return i18n.Trf(loc, "ev.ProposalCreated", who, ProposalSummaryText(s("summary")))
	case "ProposalApproved", "ProposalRejected":
		agent := s("agent_id")
		if n := names[agent]; n != "" {
			agent = n
		}
		via := ""
		if n := s("via_agent_name"); n != "" {
			via = i18n.Trf(loc, "ev.via_agent", n)
		}
		if e.Type == "ProposalRejected" {
			return i18n.Trf(loc, "ev.ProposalRejected", who, agent, ProposalSummaryText(s("summary")), s("reason")) + via
		}
		return i18n.Trf(loc, "ev.ProposalApproved", who, agent, ProposalSummaryText(s("summary"))) + via
	case "ProposalExpired":
		return i18n.Trf(loc, "ev.ProposalExpired", ProposalSummaryText(s("summary")))
	}
	return i18n.Trf(loc, "ev.generic", who, e.Type)
}

// fieldChangeSummary 把一条就地编辑动态（GoalFieldChanged / TaskFieldChanged）渲染成一句话：
// 「某人 把目标「X」的负责人从「李明」改成了「王芳」」。数据里 field 是字段名，from / to 是旧值新值，
// 引用类字段另带 from_title / to_title（当时的名字）。kind 是 goal | task，object 是「目标「X」」/「任务「X」」。
func fieldChangeSummary(e *store.EventRow, names map[string]string, loc i18n.Locale, who, kind, object string) string {
	d := e.Data
	field, _ := d["field"].(string)
	str := func(v any) string {
		switch x := v.(type) {
		case nil:
			return ""
		case string:
			return x
		case float64:
			return i18n.Number(x)
		case bool:
			if x {
				return "true"
			}
			return "false"
		}
		return TextOf(v, loc)
	}
	// 引用类字段的名字：优先动态里记下的名字，其次现在的执行者索引，最后原 ID
	name := func(idKey, titleKey string) string {
		if v, ok := d[titleKey]; ok && v != nil {
			switch x := v.(type) {
			case []any:
				parts := make([]string, 0, len(x))
				for _, item := range x {
					parts = append(parts, TextOf(item, loc))
				}
				return strings.Join(parts, i18n.Tr(loc, "sep.list"))
			default:
				if t := TextOf(x, loc); t != "" {
					return t
				}
			}
		}
		id := str(d[idKey])
		if n := names[id]; n != "" {
			return n
		}
		return id
	}
	date := func(v any) any {
		if t, err := time.ParseInLocation("2006-01-02", str(v), time.Local); err == nil {
			return i18n.Date(t)
		}
		return str(v)
	}
	label := i18n.Tr(loc, "field."+field)
	from, to := d["from"], d["to"]
	// 空列表（所需能力清空）等同于「没有」
	if arr, ok := from.([]any); ok && len(arr) == 0 {
		from = nil
	}
	if arr, ok := to.([]any); ok && len(arr) == 0 {
		to = nil
	}
	// 值的呈现：quoted 为真时套引号（名字、文字），否则裸写（日期、数字、金额）
	var fromS, toS string
	quoted := true
	switch field {
	case "title":
		return i18n.Trf(loc, "ev.fc.title", who, i18n.Tr(loc, "ev.kind."+kind), str(from), str(to))
	case "description":
		return i18n.Trf(loc, "ev.fc.description", who, object)
	case "parent_id":
		fromN, toN := name("from", "from_title"), name("to", "to_title")
		switch {
		case to == nil || toN == "":
			return i18n.Trf(loc, "ev.fc.parent_top", who, object)
		case from == nil || fromN == "":
			return i18n.Trf(loc, "ev.fc.parent_set", who, object, toN)
		default:
			return i18n.Trf(loc, "ev.fc.parent_moved", who, object, fromN, toN)
		}
	case "human_only":
		if on, _ := to.(bool); on {
			return i18n.Trf(loc, "ev.fc.human_only_on", who, object)
		}
		return i18n.Trf(loc, "ev.fc.human_only_off", who, object)
	case "status":
		switch {
		case str(to) == string(domain.GoalAchieved):
			return i18n.Trf(loc, "ev.fc.status.achieved", who, object)
		case str(to) == string(domain.GoalAbandoned):
			return i18n.Trf(loc, "ev.fc.status.abandoned", who, object)
		case str(to) == string(domain.GoalActive) && str(from) == string(domain.GoalAbandoned):
			return i18n.Trf(loc, "ev.fc.status.resumed", who, object)
		case str(to) == string(domain.GoalActive) && str(from) == string(domain.GoalAchieved):
			return i18n.Trf(loc, "ev.fc.status.unachieved", who, object)
		}
		fromS, toS = i18n.Tr(loc, "goal.status."+str(from)), i18n.Tr(loc, "goal.status."+str(to))
	case "owner_member_id", "team_id", "type_id", "reviewer_id", "goal_id", "required_capabilities", "participants":
		fromS, toS = name("from", "from_title"), name("to", "to_title")
		if field == "participants" {
			label = TextOf(d["label"], loc)
		}
	case "fields":
		label = i18n.Trf(loc, "ev.fc.custom_label", str(d["key"]))
		fromS, toS = str(from), str(to)
	case "priority":
		fromS, toS = i18n.Tr(loc, "priority."+str(from)), i18n.Tr(loc, "priority."+str(to))
	case "horizon", "confidence":
		// 时间桶与信心度只存取值，名字按当前语言现渲染（ADR 0021）
		fromS, toS = EnumTitle(loc, field, str(from)), EnumTitle(loc, field, str(to))
	case "date_precision":
		// 时间粒度的空值是「到周」而不是「没有」（ADR 0022）：从没填过或改回最细时也要念出粒度，
		// 不能说成「清空了时间粒度」
		prec := func(v any) string {
			return EnumTitle(loc, field, string(domain.EffectiveDatePrecision(domain.DatePrecision(str(v)))))
		}
		fromS, toS = prec(from), prec(to)
	case "outcome":
		// 成果指标是一句话，写全新的那句就够了，不用把旧句子也念一遍
		if to == nil || str(to) == "" {
			return i18n.Trf(loc, "ev.fc.cleared", who, object, label)
		}
		return i18n.Trf(loc, "ev.fc.rewrote_q", who, object, label, str(to))
	case "deadline", "planned_start", "planned_end":
		quoted = false
		fromS, toS = i18n.Arg(loc, date(from)).(string), i18n.Arg(loc, date(to)).(string)
	case "budget":
		quoted = false
		cur := str(d["currency"])
		if f, ok := from.(float64); ok {
			fromS = i18n.Money{Amount: f, Currency: cur}.Render(loc)
		}
		if f, ok := to.(float64); ok {
			toS = i18n.Money{Amount: f, Currency: cur}.Render(loc)
		}
	case "estimate_hours":
		quoted = false
		if f, ok := from.(float64); ok {
			fromS = i18n.Trf(loc, "unit.hours", i18n.Number(f))
		}
		if f, ok := to.(float64); ok {
			toS = i18n.Trf(loc, "unit.hours", i18n.Number(f))
		}
	case "progress_override":
		quoted = false
		if f, ok := from.(float64); ok {
			fromS = i18n.Number(f) + "%"
		}
		if f, ok := to.(float64); ok {
			toS = i18n.Number(f) + "%"
		}
	default:
		fromS, toS = str(from), str(to)
	}
	suffix := ""
	if quoted {
		suffix = "_q"
	}
	switch {
	case toS == "" && to == nil:
		return i18n.Trf(loc, "ev.fc.cleared", who, object, label)
	case fromS == "" && from == nil:
		return i18n.Trf(loc, "ev.fc.set"+suffix, who, object, label, toS)
	}
	return i18n.Trf(loc, "ev.fc.changed"+suffix, who, object, label, fromS, toS)
}

// approvalNote 是被确认后执行的动态尾巴：「经<确认人>确认」。
func ApprovalNote(e *store.EventRow, names map[string]string, loc i18n.Locale) string {
	name, _ := e.Data["approved_by_name"].(string)
	if id, _ := e.Data["approved_by"].(string); id != "" {
		if n := names[id]; n != "" && n != id {
			name = n
		}
	}
	if name == "" {
		return ""
	}
	return i18n.Trf(loc, "ev.via_approval", name)
}

// numOf 把动态数据里的数字（JSON 解出来是 float64）转成 int。
func NumOf(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	}
	return 0, false
}

// roleTitle 用组织角色表翻译角色名，缺失时回退内置。
// fieldTitles 把动态里记下的字段名换成提供方声明的显示名，拼成一句「访问令牌和接口地址」。
// 动态里只有字段名、没有字段值（配置值可能是内网地址或用户名）。
func FieldTitles(provider string, raw any, loc i18n.Locale) string {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	prov, known := directory.Lookup(provider)
	var titles []string
	for _, x := range list {
		key, _ := x.(string)
		if key == "" {
			continue
		}
		title := key
		if known {
			if f, ok := prov.Field(key); ok {
				title = f.Title.In(loc)
			} else if f, ok := prov.MessagingField(key); ok {
				title = f.Title.In(loc)
			} else if key == "proxy_url" {
				title = i18n.Tr(loc, "directory.field.proxy_url")
			}
		} else if key == "proxy_url" {
			title = i18n.Tr(loc, "directory.field.proxy_url")
		}
		titles = append(titles, title)
	}
	return strings.Join(titles, i18n.Tr(loc, "sep.list"))
}

func RoleTitle(roles map[string]i18n.Text, r string, loc i18n.Locale) string {
	if t, ok := roles[r]; ok && !t.IsZero() {
		return t.In(loc)
	}
	return domain.RoleTitle(r).In(loc)
}

// enumTitle 渲染写死词表（时间桶、信心度、时间粒度）的名字；空值没有名字。
func EnumTitle(loc i18n.Locale, kind, v string) string {
	if v == "" {
		return ""
	}
	return i18n.Tr(loc, kind+"."+v)
}

// proposalSummary 去掉待确认操作摘要句末的句号：摘要本身是一句完整的话，嵌进动态句子里时由模板负责标点，避免出现「。。」。
func ProposalSummaryText(sum string) string {
	return strings.TrimRight(strings.TrimSpace(sum), "。.")
}

// nameOr 按 ID 查名字，查不到就原样返回。
func nameOr(names map[string]string, id string) string {
	if n := names[id]; n != "" {
		return n
	}
	return id
}
