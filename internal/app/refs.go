package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 精确指代（ADR 0025 第 5 条）。一句话说完规则：
//
//	任务   #123、123、任务 ID，或标题里的一段
//	目标   目标 ID，或标题里的一段（目标没有序号，所以没有 G-编号这种写法）
//	成员   @名字、成员 / Agent 的 ID，或邮箱
//	迭代   迭代 ID，或名称里的一段
//
// 指代不明时**不许猜**：返回一句话，列出最多 5 个候选和该用哪个编号，让人自己挑。
// 一个都没匹配上也是一句话，同样的形状。这两种情况都是拒绝，不是空结果。

// RefKind 是被指代的对象种类。
type RefKind string

const (
	RefTask   RefKind = "task"
	RefGoal   RefKind = "goal"
	RefMember RefKind = "member"
	RefSprint RefKind = "sprint"
	// RefGoalType 是目标类型（ADR 0023）：类型 ID，或名字里的一段。
	RefGoalType RefKind = "goal_type"
)

// refCandidate 是一个候选：Ref 是该用的确切写法，Label 是给人看的一行。
type refCandidate struct {
	Ref   string
	Label string
}

func (c refCandidate) String() string {
	if c.Label == "" {
		return c.Ref
	}
	return c.Ref + " " + c.Label
}

// maxRefCandidates 是候选清单最多列几个：再多人也读不完，超出的用「等」带过。
const maxRefCandidates = 5

// ResolveRef 把任意写法的指代解析成对象 ID。空字符串原样返回（"不填"是合法的）。
func (a *App) ResolveRef(ctx context.Context, sess *Session, kind RefKind, ref string) (string, error) {
	s := strings.TrimSpace(ref)
	if s == "" {
		return "", nil
	}
	var id string
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		var err error
		id, err = a.resolveRef(ctx, tx, sess, kind, s)
		return err
	})
	return id, err
}

// ResolveTaskRef 把「#123」「123」「任务 ID」或标题片段换成任务 ID。
func (a *App) ResolveTaskRef(ctx context.Context, sess *Session, ref string) (string, error) {
	return a.ResolveRef(ctx, sess, RefTask, ref)
}

// ResolveRefs 一次解析多个同类指代（迭代待办批量加任务这种）。
func (a *App) ResolveRefs(ctx context.Context, sess *Session, kind RefKind, refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		id, err := a.ResolveRef(ctx, sess, kind, r)
		if err != nil {
			return nil, err
		}
		if id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

func (a *App) resolveRef(ctx context.Context, tx pgx.Tx, sess *Session, kind RefKind, ref string) (string, error) {
	switch kind {
	case RefTask:
		return a.resolveTask(ctx, tx, sess, ref)
	case RefGoal:
		return a.resolveGoal(ctx, tx, sess, ref)
	case RefMember:
		return a.resolveMember(ctx, tx, sess, ref)
	case RefSprint:
		return a.resolveSprint(ctx, tx, sess, ref)
	case RefGoalType:
		return a.resolveGoalType(ctx, tx, sess, ref)
	}
	return ref, nil
}

// ---------- 目标类型 ----------

func (a *App) resolveGoalType(ctx context.Context, tx pgx.Tx, sess *Session, ref string) (string, error) {
	types, err := a.Store.ListGoalTypes(ctx, tx)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(ref, "gtype_") {
		for _, t := range types {
			if t.ID == ref {
				return ref, nil
			}
		}
		return "", NotFound("err.goal_type_missing")
	}
	if _, rest, ok := cutRefPrefix(ref); ok {
		ref = rest
	}
	var hits []refCandidate
	for _, t := range types {
		if strings.EqualFold(strings.TrimSpace(t.Name), strings.TrimSpace(ref)) {
			return t.ID, nil
		}
		if fragmentHit(t.Name, ref) {
			hits = append(hits, refCandidate{Ref: t.ID, Label: t.Name})
		}
	}
	if len(hits) == 1 {
		return hits[0].Ref, nil
	}
	return "", refErr(sess, RefGoalType, ref, hits)
}

// ---------- 任务 ----------

func (a *App) resolveTask(ctx context.Context, tx pgx.Tx, sess *Session, ref string) (string, error) {
	if n, ok := parseTaskNumber(ref); ok {
		t, err := a.Store.TaskByNumber(ctx, tx, n)
		if err != nil {
			return "", NotFound("err.task_number", strings.TrimPrefix(ref, "#"))
		}
		return t.ID, nil
	}
	if strings.HasPrefix(ref, "tsk_") {
		if _, err := a.Store.TaskByID(ctx, tx, ref); err == nil {
			return ref, nil
		}
		return "", NotFound("err.not_found")
	}
	scope, ix, err := a.scopedIndex(ctx, tx, sess)
	if err != nil {
		return "", err
	}
	all, err := a.Store.AllTasks(ctx, tx)
	if err != nil {
		return "", err
	}
	var hits []*domain.Task
	for _, t := range ix.filterTasks(scope, all) {
		if fragmentHit(t.Title, ref) {
			hits = append(hits, t)
		}
	}
	if len(hits) == 1 {
		return hits[0].ID, nil
	}
	cands := make([]refCandidate, 0, len(hits))
	for _, t := range hits {
		cands = append(cands, refCandidate{Ref: taskNumberRef(t), Label: t.Title})
	}
	return "", refErr(sess, RefTask, ref, cands)
}

func taskNumberRef(t *domain.Task) string {
	if t.Number > 0 {
		return fmt.Sprintf("#%d", t.Number)
	}
	return t.ID
}

// ---------- 目标 ----------

func (a *App) resolveGoal(ctx context.Context, tx pgx.Tx, sess *Session, ref string) (string, error) {
	if strings.HasPrefix(ref, "goal_") {
		if _, err := a.Store.GoalByID(ctx, tx, ref); err == nil {
			return ref, nil
		}
		return "", NotFound("err.goal_missing")
	}
	// 「目标:标题」这种写法把冒号后面的当片段
	if _, rest, ok := cutRefPrefix(ref); ok {
		ref = rest
	}
	goals, err := a.Store.ListGoals(ctx, tx)
	if err != nil {
		return "", err
	}
	var hits []*domain.Goal
	for _, g := range goals {
		if !sess.CanSeeCollabTeam(g.TeamID) && g.TeamID != "" {
			continue
		}
		if fragmentHit(g.Title, ref) {
			hits = append(hits, g)
		}
	}
	if len(hits) == 1 {
		return hits[0].ID, nil
	}
	cands := make([]refCandidate, 0, len(hits))
	for _, g := range hits {
		cands = append(cands, refCandidate{Ref: g.ID, Label: g.Title})
	}
	return "", refErr(sess, RefGoal, ref, cands)
}

// ---------- 成员与 Agent ----------

func (a *App) resolveMember(ctx context.Context, tx pgx.Tx, sess *Session, ref string) (string, error) {
	if ref == "me" {
		return sess.Actor.ID, nil
	}
	if strings.HasPrefix(ref, "mem_") || strings.HasPrefix(ref, "agt_") {
		names, err := a.Store.ExecutorNames(ctx, tx)
		if err != nil {
			return "", err
		}
		if names[ref] != "" {
			return ref, nil
		}
		return "", NotFound("err.member_missing")
	}
	// 邮箱：先找账号，再找这个组织里对应的成员
	if strings.Contains(ref, "@") && strings.Contains(ref, ".") && !strings.HasPrefix(ref, "@") {
		acc, _, err := a.Store.AccountByEmail(ctx, a.Store.Pool, strings.ToLower(ref))
		if err == nil {
			if m, err := a.Store.MemberByAccount(ctx, tx, sess.OrgID, acc.ID); err == nil {
				return m.ID, nil
			}
		}
		return "", NotFound("err.member_missing")
	}
	frag := strings.TrimPrefix(ref, "@")
	names, err := a.Store.ExecutorNames(ctx, tx)
	if err != nil {
		return "", err
	}
	var hits []refCandidate
	for id, name := range names {
		if fragmentHit(name, frag) {
			hits = append(hits, refCandidate{Ref: id, Label: a.memberLabel(ctx, tx, id, name)})
		}
	}
	if len(hits) == 1 {
		return hits[0].Ref, nil
	}
	sortCandidates(hits)
	return "", refErr(sess, RefMember, frag, hits)
}

// memberLabel 是候选里那一行的人名，成员再带上邮箱（邮箱本身也是可以直接用的指代）。
func (a *App) memberLabel(ctx context.Context, tx pgx.Tx, id, name string) string {
	if !strings.HasPrefix(id, "mem_") {
		return name
	}
	m, err := a.Store.MemberByID(ctx, tx, id)
	if err != nil {
		return name
	}
	acc, err := a.Store.AccountByID(ctx, tx, m.AccountID)
	if err != nil || acc.Email == "" {
		return name
	}
	return fmt.Sprintf("%s（%s）", name, acc.Email)
}

// ---------- 迭代 ----------

func (a *App) resolveSprint(ctx context.Context, tx pgx.Tx, sess *Session, ref string) (string, error) {
	if strings.HasPrefix(ref, "spr_") {
		if _, err := a.Store.SprintByID(ctx, tx, ref); err == nil {
			return ref, nil
		}
		return "", NotFound("err.sprint_missing")
	}
	if _, rest, ok := cutRefPrefix(ref); ok {
		ref = rest
	}
	sprints, err := a.Store.ListSprints(ctx, tx, store.SprintFilter{})
	if err != nil {
		return "", err
	}
	var hits []refCandidate
	for _, sp := range sprints {
		if fragmentHit(sp.Name, ref) {
			hits = append(hits, refCandidate{Ref: sp.ID, Label: sp.Name})
		}
	}
	if len(hits) == 1 {
		return hits[0].Ref, nil
	}
	return "", refErr(sess, RefSprint, ref, hits)
}

// ---------- 公共 ----------

// fragmentHit 判断名字里带不带这一段（忽略大小写与两端空白）。
func fragmentHit(name, frag string) bool {
	frag = strings.TrimSpace(frag)
	if frag == "" {
		return false
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(frag))
}

// cutRefPrefix 认「目标:标题」「迭代：名称」这种写法，返回冒号前后两段。
func cutRefPrefix(ref string) (head, rest string, ok bool) {
	for _, sep := range []string{":", "："} {
		if h, r, found := strings.Cut(ref, sep); found && strings.TrimSpace(r) != "" {
			return strings.TrimSpace(h), strings.TrimSpace(r), true
		}
	}
	return "", ref, false
}

// sortCandidates 让候选清单的顺序稳定（map 遍历是随机的）。
func sortCandidates(list []refCandidate) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Ref < list[j-1].Ref; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// refErr 把"没找到"与"不止一个"变成同一个形状的一句话。
func refErr(sess *Session, kind RefKind, frag string, cands []refCandidate) error {
	if len(cands) == 0 {
		return NotFound("ref.none."+string(kind), frag)
	}
	loc := sess.Loc()
	shown := cands
	if len(shown) > maxRefCandidates {
		shown = shown[:maxRefCandidates]
	}
	parts := make([]string, 0, len(shown))
	for _, c := range shown {
		parts = append(parts, c.String())
	}
	list := strings.Join(parts, i18n.Tr(loc, "sep.list"))
	if len(cands) > len(shown) {
		list += i18n.Tr(loc, "ref.etc")
	}
	return Bad("ref.ambiguous."+string(kind), len(cands), frag, list)
}
