package store

import (
	"context"
	"encoding/json"

	"github.com/teemo/axiomos/internal/domain"
)

// ---------- 显示偏好（功能规划第 4 项） ----------

// PreferenceRow 是一条偏好记录：属于一个成员或一个角色。
type PreferenceRow struct {
	Kind    string // member | role
	Subject string // 成员 ID 或角色名
	Patch   domain.PreferencePatch
}

func scanPreference(r interface{ Scan(...any) error }) (*PreferenceRow, error) {
	row := &PreferenceRow{}
	var data []byte
	err := r.Scan(&row.Kind, &row.Subject, &data)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(data, &row.Patch)
	return row, nil
}

// Preference 读一条记录。
func (s *Store) Preference(ctx context.Context, q Querier, kind, subject string) (*PreferenceRow, error) {
	return scanPreference(q.QueryRow(ctx, `select subject_kind,subject_id,data from preferences where subject_kind=$1 and subject_id=$2`, kind, subject))
}

// ListRolePreferences 列出组织里全部角色的偏好记录。
func (s *Store) ListRolePreferences(ctx context.Context, q Querier) ([]*PreferenceRow, error) {
	rows, err := q.Query(ctx, `select subject_kind,subject_id,data from preferences where subject_kind='role' order by subject_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PreferenceRow
	for rows.Next() {
		r, err := scanPreference(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutPreference 写入或覆盖一条记录。
func (s *Store) PutPreference(ctx context.Context, q Querier, orgID, kind, subject string, p domain.PreferencePatch) error {
	b, _ := json.Marshal(p)
	_, err := q.Exec(ctx, `insert into preferences(org_id,subject_kind,subject_id,data,updated_at) values($1,$2,$3,$4,now())
		on conflict (org_id,subject_kind,subject_id) do update set data=excluded.data, updated_at=now()`, orgID, kind, subject, b)
	return err
}

// DeletePreference 删掉一条记录。
func (s *Store) DeletePreference(ctx context.Context, q Querier, kind, subject string) error {
	_, err := q.Exec(ctx, `delete from preferences where subject_kind=$1 and subject_id=$2`, kind, subject)
	return err
}
