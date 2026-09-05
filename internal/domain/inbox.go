package domain

// 待我处理（DESIGN.md §12）用到的两条流程规则。只认状态类型与步骤的声明，不认状态名（ADR 0005）：
// 「等我验收」= 任务停在等待类型的状态上，且从这里出发有一步需要「验收」授权、而我满足它的触发者规则；
// 「等我答复」= 任务停在等待类型的状态上，且从这里出发有一步要求写评论、不需要「验收」授权、而我满足它的触发者规则
//（这就是「答复并恢复」；「解除阻塞」不要求评论，「验收打回」带验收授权，都不算）。

// outgoing 返回从任务当前状态出发的全部步骤。
func outgoing(t *Task, wf *Workflow) []Transition {
	cur := wf.State(t.State)
	var out []Transition
	for _, tr := range wf.Transitions {
		for _, f := range tr.From {
			if f == t.State || (f == "*" && cur != nil && !cur.Label.IsTerminal()) {
				out = append(out, tr)
				break
			}
		}
	}
	return out
}

// waitingState 判断任务是否停在等待类型的状态上。
func waitingState(t *Task, tt *TaskType) bool {
	if tt == nil {
		return false
	}
	st := tt.Workflow.State(t.State)
	return st != nil && st.Label == LabelWaiting
}

// AwaitsReviewBy 判断任务是否在等 actor 验收。
func AwaitsReviewBy(t *Task, tt *TaskType, actor *Executor) bool {
	if !waitingState(t, tt) {
		return false
	}
	for _, tr := range outgoing(t, &tt.Workflow) {
		if tr.Grant == GrantReview && matchesBy(t, actor, tr.By) {
			return true
		}
	}
	return false
}

// AwaitsReplyFrom 判断任务是否在等 actor 答复。
func AwaitsReplyFrom(t *Task, tt *TaskType, actor *Executor) bool {
	if !waitingState(t, tt) {
		return false
	}
	for _, tr := range outgoing(t, &tt.Workflow) {
		if tr.Grant == GrantReview || !contains(tr.Requires, "comment") {
			continue
		}
		if matchesBy(t, actor, tr.By) {
			return true
		}
	}
	return false
}

// OpenQuestion 返回等 actor 答复的那条提问：最后一条评论（不含工作日志），且不是 actor 自己写的。
// actor 已经回过话（最后一条是他的）就视为已答复，返回 nil。
func OpenQuestion(t *Task, actor *Executor) *Comment {
	for i := len(t.Comments) - 1; i >= 0; i-- {
		c := t.Comments[i]
		if c.IsNote {
			continue
		}
		if isPrincipal(actor, c.ByID) {
			return nil
		}
		return &c
	}
	return nil
}
