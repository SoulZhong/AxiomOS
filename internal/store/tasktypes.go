package store

import (
	"context"
	"encoding/json"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
)

// SaveTaskType 保存任务类型的新版本并设为当前版本。
func (s *Store) SaveTaskType(ctx context.Context, q Querier, orgID string, tt *domain.TaskType) error {
	var cur int
	_ = q.QueryRow(ctx, `select coalesce(max(version),0) from task_types where org_id=$1 and name=$2`, orgID, tt.Name).Scan(&cur)
	tt.Workflow.Version = cur + 1
	def, err := json.Marshal(tt)
	if err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `insert into task_types(org_id,name,version,title,built_in,definition) values($1,$2,$3,$4,$5,$6)`,
		orgID, tt.Name, tt.Workflow.Version, tt.Title.In(i18n.Default), tt.BuiltIn, def); err != nil {
		return err
	}
	_, err = q.Exec(ctx, `insert into task_type_current(org_id,name,version) values($1,$2,$3) on conflict (org_id,name) do update set version=excluded.version`, orgID, tt.Name, tt.Workflow.Version)
	return err
}

func (s *Store) TaskType(ctx context.Context, q Querier, name string, version int) (*domain.TaskType, error) {
	var def []byte
	err := q.QueryRow(ctx, `select definition from task_types where name=$1 and version=$2`, name, version).Scan(&def)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	tt := &domain.TaskType{}
	if err := json.Unmarshal(def, tt); err != nil {
		return nil, err
	}
	return tt, nil
}

func (s *Store) CurrentTaskType(ctx context.Context, q Querier, name string) (*domain.TaskType, error) {
	var v int
	err := q.QueryRow(ctx, `select version from task_type_current where name=$1`, name).Scan(&v)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.TaskType(ctx, q, name, v)
}

// ListCurrentTaskTypes 列出每种任务类型的当前版本。
func (s *Store) ListCurrentTaskTypes(ctx context.Context, q Querier) ([]*domain.TaskType, error) {
	rows, err := q.Query(ctx, `select t.definition from task_types t join task_type_current c on c.org_id=t.org_id and c.name=t.name and c.version=t.version order by t.built_in desc, t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.TaskType
	for rows.Next() {
		var def []byte
		if err := rows.Scan(&def); err != nil {
			return nil, err
		}
		tt := &domain.TaskType{}
		if err := json.Unmarshal(def, tt); err != nil {
			return nil, err
		}
		out = append(out, tt)
	}
	return out, rows.Err()
}

// TaskTypesByVersion 按 (name, version) 批量装载，用于给内核提供关联任务的类型。
func (s *Store) TaskTypesFor(ctx context.Context, q Querier, tasks []*domain.Task) (map[string]*domain.TaskType, error) {
	out := map[string]*domain.TaskType{}
	for _, t := range tasks {
		if _, ok := out[t.TypeName]; ok {
			continue
		}
		tt, err := s.TaskType(ctx, q, t.TypeName, t.TypeVersion)
		if err != nil {
			return nil, err
		}
		out[t.TypeName] = tt
	}
	return out, nil
}
