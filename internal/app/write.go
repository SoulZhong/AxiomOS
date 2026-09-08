package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// 只看不做与幂等键（ADR 0025 第 2、3 条）。两个开关都挂在会话上，HTTP 与 MCP 各自把
// 自己那一侧的参数（?dry_run=1 / idempotency_key）填进来，写操作本身不必知道调用者是谁。

// WriteOptions 是一次写操作的两个开关。
type WriteOptions struct {
	// DryRun 为真时：照常做全部校验与权限判断，然后什么都不写，返回一句「我将要做什么」。
	DryRun bool
	// IdempotencyKey 是客户端自己生成的键（≤64 字符）：24 小时内同一个键只生效一次。
	IdempotencyKey string
}

// MaxIdempotencyKeyLen 是幂等键的长度上限。
const MaxIdempotencyKeyLen = 64

// WithWrite 返回一个带写操作开关的会话副本（HTTP 与 MCP 层用）。
func (s *Session) WithWrite(o WriteOptions) *Session {
	if s == nil {
		return nil
	}
	c := *s
	c.Write = o
	return &c
}

// ---------- 只看不做 ----------

// DryRunResult 表示这次写操作只是"看一看"：什么都没写，Will 是将要发生什么的一句话。
// 它实现 error，沿着与「待确认操作」相同的返回路径一路传到 HTTP 与 MCP 层。
type DryRunResult struct {
	Will string   // 完整句子（按请求者语言）
	Msg  i18n.Msg // 未渲染的消息，便于换语言
}

func (d *DryRunResult) Error() string { return d.Will }

// AsDryRun 判断一个错误是不是"只看不做"的结果。
func AsDryRun(err error) (*DryRunResult, bool) {
	var d *DryRunResult
	if errors.As(err, &d) {
		return d, true
	}
	return nil, false
}

// willText 把若干个短句拼成一句「会……，并……」。
type willText struct{ clauses []i18n.Msg }

// Render 实现 i18n.Renderable。
func (w willText) Render(l i18n.Locale) string {
	parts := make([]string, 0, len(w.clauses))
	for _, c := range w.clauses {
		if s := c.Render(l); s != "" {
			parts = append(parts, s)
		}
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	last := parts[len(parts)-1]
	head := parts[:len(parts)-1]
	if l == i18n.EnUS {
		if len(head) == 1 {
			return head[0] + " and " + last
		}
		return strings.Join(head, ", ") + ", and " + last
	}
	return strings.Join(head, "，") + "，并" + last
}

// dryRun 把若干短句变成给调用方的返回值（一个 *DryRunResult）。
func dryRun(sess *Session, clauses ...i18n.Msg) error {
	w := willText{clauses: clauses}
	m := i18n.M("dry.will", w)
	if len(w.clauses) == 0 {
		m = i18n.M("dry.nothing")
	}
	return &DryRunResult{Will: m.Render(sess.Loc()), Msg: m}
}

// clauseList 把若干短句用逗号连起来（不加「并」）：批量操作说的是"改了几个、跳过几个"，
// 是并列的两笔账，不是一件事接着另一件事。
type clauseList struct{ clauses []i18n.Msg }

// Render 实现 i18n.Renderable。
func (c clauseList) Render(l i18n.Locale) string {
	parts := make([]string, 0, len(c.clauses))
	for _, m := range c.clauses {
		if s := m.Render(l); s != "" {
			parts = append(parts, s)
		}
	}
	if l == i18n.EnUS {
		return strings.Join(parts, ", ")
	}
	return strings.Join(parts, "，")
}

// dryRunList 是批量写操作的只看不做结果：「会把 3 个目标的时间桶改成「现在」，另有 1 个目标你没有权限改。」
func dryRunList(sess *Session, clauses ...i18n.Msg) error {
	c := clauseList{clauses: clauses}
	m := i18n.M("dry.will", c)
	if c.Render(sess.Loc()) == "" {
		m = i18n.M("dry.nothing")
	}
	return &DryRunResult{Will: m.Render(sess.Loc()), Msg: m}
}

// dryRunPending 是「这一步需要人确认」时的只看不做结果：真做的时候不会立刻生效，
// 而是记一条待确认操作。说清楚这一点，人才不会以为点一下就成了。
func dryRunPending(sess *Session, ownerName string, summary i18n.Msg) error {
	m := i18n.M("dry.will_pending", ownerName, summary)
	return &DryRunResult{Will: m.Render(sess.Loc()), Msg: m}
}

// refuseDryRun 是给"不支持只看不做"的写路径用的挡板（ADR 0025 第 2 条）。
//
// 兜底放在 a.insertEvents 里，可它只挡得住会写动态的路径；组织配置类的改动（角色、能力标签、
// 计价、组织信息、待确认操作的裁决、设备码的同意与拒绝……）有些不记动态，兜底够不着，
// 所以在入口处自己挡一下：宁可照实说一句"还不支持"，也不能一边说"只是看看"一边把东西写进去。
func refuseDryRun(sess *Session) error {
	if sess != nil && sess.Write.DryRun {
		return Bad("err.dry_run_unsupported")
	}
	return nil
}

// taskRefText 把任务写成「#12 登录页改版」，没有序号时只写标题。
func taskRefText(t *domain.Task) string {
	if t == nil {
		return ""
	}
	if t.Number > 0 {
		return fmt.Sprintf("#%d %s", t.Number, t.Title)
	}
	return t.Title
}

// willClauses 把内核算出来的结果翻译成一串「将要发生什么」的短句。
// 只读内核的产物，所以说的就是真做时会发生的事，不是另写一套说法。
func (a *App) willClauses(ctx context.Context, tx pgx.Tx, sess *Session, c *domain.Context, o *domain.Outcome) []i18n.Msg {
	if o == nil {
		return nil
	}
	t := o.Task
	if t == nil {
		t = c.Task
	}
	ref := taskRefText(t)
	stateTitle := func(name string) any {
		if name == "$previous" {
			name = c.Task.PreviousState
		}
		if st := c.Type.Workflow.State(name); st != nil {
			return st.Title
		}
		return name
	}
	name := func(id string) string { return a.executorName(ctx, tx, id) }
	var out []i18n.Msg
	for _, e := range o.Events {
		s := func(k string) string { v, _ := e.Data[k].(string); return v }
		switch e.Type {
		case "TaskCreated":
			out = append(out, i18n.M("will.task.create", ref))
		case "TaskClaimed":
			out = append(out, i18n.M("will.task.claim", ref))
		case "TaskAssigned":
			if to := s("to"); to != "" {
				out = append(out, i18n.M("will.task.assign", ref, name(to)))
			} else {
				out = append(out, i18n.M("will.task.unassign", ref))
			}
		case "TaskTransitioned":
			out = append(out, i18n.M("will.task.transition", ref, stateTitle(t.State)))
			out = append(out, a.willNextArtifacts(c, t)...)
		case "RunStarted":
			out = append(out, i18n.M("will.run.start"))
		case "RunEnded":
			out = append(out, i18n.M("will.run.end"))
		case "UsageReported":
			out = append(out, i18n.M("will.run.usage"))
		case "ArtifactAttached":
			out = append(out, i18n.M("will.task.artifact", ref, s("title")))
		case "CommentAdded":
			out = append(out, i18n.M("will.task.comment", ref))
		case "NoteAdded":
			out = append(out, i18n.M("will.task.note", ref))
		case "TasksLinked":
			out = append(out, i18n.M("will.task.link", ref))
		case "RelationRemoved":
			out = append(out, i18n.M("will.task.unlink", ref))
		case "TaskSentToBacklog":
			out = append(out, i18n.M("will.task.backlog", ref))
		case "PointsChanged":
			out = append(out, i18n.M("will.task.points", ref))
		case "TaskAddedToSprint":
			out = append(out, i18n.M("will.task.sprint_add", ref, s("name")))
		case "TaskRemovedFromSprint":
			out = append(out, i18n.M("will.task.sprint_remove", ref, s("name")))
		case "TaskFieldChanged", "TaskUpdated":
			out = append(out, i18n.M("will.task.update", ref))
		}
	}
	return out
}

// willNextArtifacts 说明推进之后那一步要附上哪种交付物（还没附的才说）。
func (a *App) willNextArtifacts(c *domain.Context, t *domain.Task) []i18n.Msg {
	have := map[string]bool{}
	for _, art := range t.Artifacts {
		have[art.Type] = true
	}
	seen := map[string]bool{}
	var out []i18n.Msg
	for _, tr := range c.Type.Workflow.Transitions {
		if !transitionFrom(tr, t.State) {
			continue
		}
		for _, req := range tr.Requires {
			typ, ok := strings.CutPrefix(req, "artifact:")
			if !ok || have[typ] || seen[typ] {
				continue
			}
			seen[typ] = true
			out = append(out, i18n.M("will.then_artifact", domain.ArtifactTitle(typ)))
		}
	}
	return out
}

func transitionFrom(tr domain.Transition, state string) bool {
	for _, f := range tr.From {
		if f == state {
			return true
		}
	}
	return false
}

// ---------- 幂等键 ----------

// Repeated 表示这次调用命中了 24 小时内用过的幂等键：什么都没有再写一次，
// 带回来的是第一次的结果。它同样实现 error，沿现有返回路径传到 HTTP 与 MCP 层。
type Repeated struct {
	Message   string          // 「这次没有重复创建，返回的是第一次的结果。」
	Result    json.RawMessage // 第一次的结果，原样
	Ref       string          // 第一次写出来的对象 ID
	Endpoint  string
	CreatedAt time.Time
}

func (r *Repeated) Error() string { return r.Message }

// AsRepeated 判断一个错误是不是"命中幂等键"。
func AsRepeated(err error) (*Repeated, bool) {
	var rp *Repeated
	if errors.As(err, &rp) {
		return rp, true
	}
	return nil, false
}

// argsHash 是归一化参数的指纹：同一个键换了参数要被拒绝，靠它认出来。
// json.Marshal 对 map 的键会排序，所以同样的参数得到同样的字节。
func argsHash(endpoint string, args any) string {
	b, err := json.Marshal(args)
	if err != nil {
		b = []byte(fmt.Sprint(args))
	}
	sum := sha256.Sum256(append([]byte(endpoint+"\x00"), b...))
	return hex.EncodeToString(sum[:])
}

// idempotent 给一次写操作套上幂等键。没带键时原样执行。
//
// 顺序是「先占位再干活」（ADR 0025 第 3 条）：
//  1. 先在自己的短事务里把键插进去，状态记成"还在干"。插得进去就归自己，接着真做。
//  2. 插不进去说明这个键有主了：干完了（同参数返回第一次的结果，不同参数拒绝）；
//     还在干且没过 60 秒的租约（回一句「上一次的调用还在进行中」）；
//     还在干但过了租约（上一次的进程死了，接手继续做）。
//  3. 做成了把占位改成"干完了"并存下结果；做砸了把占位删掉——失败本来就该允许重试。
//
// 先占位是关键：两个带同一个键的请求同时进来，数据库的唯一索引在**副作用发生之前**就把
// 后来的那个挡在门外，而不是等两边都写完了再去挡一条记录。
func idempotent[T any](ctx context.Context, a *App, sess *Session, endpoint string, args any, fn func() (T, error)) (T, error) {
	var zero T
	key := strings.TrimSpace(sess.Write.IdempotencyKey)
	if key == "" {
		return fn()
	}
	if len([]rune(key)) > MaxIdempotencyKeyLen {
		return zero, Bad("err.idem_key_len", MaxIdempotencyKeyLen)
	}
	hash := argsHash(endpoint, args)

	// 只看不做一个位都不占：预演过的键，真做的时候还要能用。只读一眼，照实说会发生什么。
	if sess.Write.DryRun {
		prev, err := a.idempotencyRecord(ctx, sess, key)
		if err != nil {
			return zero, err
		}
		if prev != nil {
			if prev.ArgsHash != hash {
				return zero, Bad("err.idem_conflict", key)
			}
			if prev.Fresh(time.Now()) {
				return zero, Conflict("err.idem_in_progress")
			}
			if prev.State == store.IdemDone {
				return zero, &DryRunResult{Will: i18n.Trf(sess.Loc(), "dry.idem_repeat"), Msg: i18n.M("dry.idem_repeat")}
			}
		}
		return fn()
	}

	rec := &store.IdempotencyRecord{OrgID: sess.OrgID, ExecutorID: sess.Actor.ID, Key: key,
		Endpoint: endpoint, ArgsHash: hash}
	var blocker *store.IdempotencyRecord
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		ok, prev, err := a.Store.ReserveIdempotencyKey(ctx, tx, rec)
		if err != nil {
			return err
		}
		if !ok {
			blocker = prev
		}
		return nil
	}); err != nil {
		return zero, err
	}
	if blocker != nil {
		if blocker.ArgsHash != hash {
			return zero, Bad("err.idem_conflict", key)
		}
		if blocker.State == store.IdemDone {
			return zero, repeatedOf(sess, blocker)
		}
		return zero, Conflict("err.idem_in_progress")
	}

	out, err := fn()
	if err != nil {
		// 没干成：把占位撤掉，同一个键还能重试。
		if rErr := a.tx(ctx, sess, func(tx pgx.Tx) error {
			return a.Store.ReleaseIdempotencyKey(ctx, tx, rec.ID)
		}); rErr != nil {
			log.Printf("幂等键 %s 的占位没能撤掉: %v", key, rErr)
		}
		return zero, err
	}
	body, mErr := json.Marshal(out)
	if mErr != nil {
		body = []byte("null")
	}
	rec.ResultRef, rec.Result = resultRef(out), body
	if err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		return a.Store.FinishIdempotencyKey(ctx, tx, rec)
	}); err != nil {
		return zero, err
	}
	return out, nil
}

func (a *App) idempotencyRecord(ctx context.Context, sess *Session, key string) (*store.IdempotencyRecord, error) {
	var rec *store.IdempotencyRecord
	err := a.tx(ctx, sess, func(tx pgx.Tx) error {
		r, err := a.Store.IdempotencyRecordByKey(ctx, tx, sess.Actor.ID, key)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		rec = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func repeatedOf(sess *Session, r *store.IdempotencyRecord) error {
	return &Repeated{Message: i18n.Trf(sess.Loc(), "idem.repeated"), Result: r.Result, Ref: r.ResultRef,
		Endpoint: r.Endpoint, CreatedAt: r.CreatedAt}
}

// resultRef 从结果里挖出对象 ID（挖不出来就空着）。
func resultRef(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return ""
	}
	if id, ok := m["id"].(string); ok {
		return id
	}
	if id, ok := m["task_id"].(string); ok {
		return id
	}
	return ""
}

// SweepIdempotencyKeys 清掉过期的幂等键（由后台巡检调用）。
func (a *App) SweepIdempotencyKeys(ctx context.Context, orgID string) (int, error) {
	n := 0
	err := a.Store.WithOrg(ctx, orgID, func(tx pgx.Tx) error {
		var err error
		n, err = a.Store.SweepIdempotencyKeys(ctx, tx, time.Now())
		return err
	})
	return n, err
}
