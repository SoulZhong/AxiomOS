package domain

import (
	"strings"

	"github.com/teemo/axiomos/internal/i18n"
)

// ValidationError 是一条可翻译的校验问题。
type ValidationError struct{ Msg i18n.Msg }

func (e *ValidationError) Error() string { return e.Msg.Render(i18n.Default) }

// RenderErrors 按语言渲染校验问题。
func RenderErrors(l i18n.Locale, errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		if ve, ok := e.(*ValidationError); ok {
			out = append(out, ve.Msg.Render(l))
		} else {
			out = append(out, e.Error())
		}
	}
	return out
}

// 内核认识的前提条件（docs/spec §3.2）。
var knownConditions = map[string]bool{
	"deps_done":    true,
	"comment":      true,
	"no_open_bugs": true,
	"result":       true,
}

var validLabels = map[Label]bool{
	LabelPending: true, LabelActive: true, LabelWaiting: true, LabelTerminalSuccess: true, LabelTerminalFailure: true,
}

// Validate 按规格 §7 校验任务类型（含流程）。返回全部问题，空切片表示通过。
func Validate(tt *TaskType) []error {
	var errs []error
	add := func(key string, a ...any) { errs = append(errs, &ValidationError{Msg: i18n.M(key, a...)}) }
	if tt.Name == "" {
		add("val.no_name")
	}
	if tt.Title.IsZero() {
		add("val.no_title")
	}
	slots := map[string]bool{}
	for _, p := range tt.Participants {
		if slots[p.Slot] {
			add("val.slot_dup", p.Slot)
		}
		slots[p.Slot] = true
		if p.Role == "" {
			add("val.slot_no_role", p.Slot)
		}
	}

	wf := &tt.Workflow
	states := map[string]*State{}
	hasSuccess := false
	for i := range wf.States {
		s := &wf.States[i]
		if states[s.Name] != nil {
			add("val.state_dup", s.Name)
		}
		states[s.Name] = s
		if !validLabels[s.Label] {
			add("val.bad_label", s.Name, string(s.Label))
		}
		if s.Claimable && s.Label != LabelPending {
			add("val.claimable", s.Name)
		}
		if s.Weight != nil && (*s.Weight < 0 || *s.Weight > 100) {
			add("val.weight", s.Name)
		}
		if s.WIPLimit < 0 {
			add("val.wip_limit", s.Name)
		}
		if s.Label == LabelTerminalSuccess {
			hasSuccess = true
		}
	}
	if init := states[wf.Initial]; init == nil {
		add("val.initial_missing", wf.Initial)
	} else if init.Label != LabelPending {
		add("val.initial_label", wf.Initial)
	}
	if !hasSuccess {
		add("val.no_success")
	}

	names := map[string]bool{}
	outOfActive := map[string]bool{}
	for _, tr := range wf.Transitions {
		if names[tr.Name] {
			add("val.tr_dup", tr.Name)
		}
		names[tr.Name] = true
		for _, f := range tr.From {
			if f == "*" {
				for n, s := range states {
					if s.Label == LabelActive {
						outOfActive[n] = true
					}
				}
				continue
			}
			if states[f] == nil {
				add("val.tr_from", tr.Name, f)
			} else if states[f].Label == LabelActive {
				outOfActive[f] = true
			}
		}
		if tr.To != "$previous" && states[tr.To] == nil {
			add("val.tr_to", tr.Name, tr.To)
		}
		if len(tr.By) == 0 {
			add("val.tr_no_by", tr.Name)
		}
		for _, b := range tr.By {
			switch {
			case b == "creator", b == "assignee", b == "reviewer", b == "anyone":
			case strings.HasPrefix(b, "participant:"):
				if !slots[strings.TrimPrefix(b, "participant:")] {
					add("val.tr_bad_participant", tr.Name, b)
				}
			case strings.HasPrefix(b, "role:"):
			default:
				add("val.tr_bad_by", tr.Name, b)
			}
		}
		for _, c := range tr.Requires {
			if strings.HasPrefix(c, "artifact:") {
				continue
			}
			if !knownConditions[c] {
				add("val.tr_bad_cond", tr.Name, c)
			}
		}
		// 外部事件触发（ADR 0020）：来源与事件名必须是内核认识的
		if tg := tr.TriggeredBy; tg != nil {
			if !IsExternalSource(tg.Source) {
				add("val.tr_bad_trigger_source", tr.Name, tg.Source)
			}
			if !IsExternalEvent(tg.Event) {
				add("val.tr_bad_trigger_event", tr.Name, tg.Event)
			}
		}
		if strings.HasPrefix(tr.AssignTo, "participant:") {
			if !slots[strings.TrimPrefix(tr.AssignTo, "participant:")] {
				add("val.tr_bad_assign", tr.Name, tr.AssignTo)
			}
		} else if tr.AssignTo != "" && tr.AssignTo != "creator" {
			add("val.tr_bad_assign_rule", tr.Name, tr.AssignTo)
		}
	}
	for n, s := range states {
		if s.Label == LabelActive && !outOfActive[n] {
			add("val.active_dead_end", n)
		}
	}
	return errs
}
