package app

import (
	"time"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// fieldChange 是一次就地编辑里某个字段的改动。每个改动各产生一条动态（GoalFieldChanged / TaskFieldChanged），
// 动态数据里带 field / from / to；引用类字段（人、团队、目标、任务、迭代）另带 from_title / to_title，
// 这样句子里写的是当时的名字，之后改名也不影响历史。
type fieldChange struct {
	Field     string
	From, To  any
	FromTitle any // string 或 []i18n.Text（能力标签）
	ToTitle   any
	Label     i18n.Text      // 参与角色位置名等需要展示名的字段
	Extra     map[string]any // 货币、位置名等附加信息
}

// event 组装一条动态。base 是对象级信息（goal_id、title），每条动态都带。
func (c fieldChange) event(kind string, sess *Session, taskID string, base map[string]any) domain.Event {
	data := map[string]any{"field": c.Field, "from": c.From, "to": c.To}
	for k, v := range base {
		data[k] = v
	}
	if c.FromTitle != nil {
		data["from_title"] = c.FromTitle
	}
	if c.ToTitle != nil {
		data["to_title"] = c.ToTitle
	}
	if !c.Label.IsZero() {
		data["label"] = c.Label
	}
	for k, v := range c.Extra {
		data[k] = v
	}
	return domain.Event{Type: kind, TaskID: taskID, ActorID: sess.Actor.ID, At: time.Now(), Data: data}
}

// dateVal 把日期写成动态里的 YYYY-MM-DD；空为 nil。
func dateVal(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func sameFloat(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func floatVal(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// nilIfEmpty 让空字符串在动态里表现为「没有」。
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
