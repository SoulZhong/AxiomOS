// Package store 是 PostgreSQL 存储层。每个组织内的读写都必须在 WithOrg 开启的事务里进行，
// 事务开头设置 app.org_id，数据库的行级安全据此隔离（ADR 0001）。
package store

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base32"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed all:migrations
var migrationFS embed.FS

type Store struct {
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return &Store{Pool: pool}, nil
}

// Migrate 按文件名顺序执行 migrations/*.sql，记录到 schema_migrations。
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `create table if not exists schema_migrations (name text primary key, applied_at timestamptz not null default now())`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		var exists bool
		if err := s.Pool.QueryRow(ctx, `select exists(select 1 from schema_migrations where name=$1)`, n).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + n)
		if err != nil {
			return err
		}
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("迁移 %s 失败: %w", n, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations(name) values($1)`, n); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// WithOrg 在组织上下文里执行一个事务。
func (s *Store) WithOrg(ctx context.Context, orgID string, fn func(tx pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select set_config('app.org_id', $1, true)`, orgID); err != nil {
		return err
	}
	// 切到非超级用户角色，行级安全才会生效（超级用户会绕过策略）
	if _, err := tx.Exec(ctx, `set local role axiomos_app`); err != nil {
		return fmt.Errorf("切换到应用角色失败（行级安全依赖它）: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// NewID 生成带前缀的短随机 ID，如 tsk_3k9f2a8bqz。
func NewID(prefix string) string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

// ErrNotFound 表示没有这条记录。
var ErrNotFound = fmt.Errorf("没有找到这条记录")

func isNoRows(err error) bool { return err == pgx.ErrNoRows }

// Querier 是 pool 与 tx 共有的查询接口。
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
