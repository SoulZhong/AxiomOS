// axiomd 是 AxiomOS 的唯一后端进程：HTTP API + MCP + 静态托管前端。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teemo/axiomos/internal/api"
	"github.com/teemo/axiomos/internal/app"
	"github.com/teemo/axiomos/internal/directory"
	"github.com/teemo/axiomos/internal/mcp"
	"github.com/teemo/axiomos/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	ctx := context.Background()
	dbURL := env("DATABASE_URL", "postgres://axiomos:axiomos@localhost:5439/axiomos?sslmode=disable")
	addr := env("ADDR", ":8080")
	webDir := env("WEB_DIR", "web/out")
	seedDemo := env("SEED_DEMO", "1") == "1"

	st, err := store.Open(ctx, dbURL)
	if err != nil {
		log.Fatalf("数据库: %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("迁移: %v", err)
	}
	a := app.New(st)
	a.PublicURL = strings.TrimRight(env("PUBLIC_URL", "http://localhost:8080"), "/")
	// 组织凭据（外部目录的保密字段等）的加密密钥；没配就用确定性的开发密钥，生产必须配置（ADR 0014、0017）
	if k := os.Getenv(directory.SecretKeyEnv); k != "" {
		key, err := directory.ParseKey(k)
		if err != nil {
			log.Fatalf("%s: %v", directory.SecretKeyEnv, err)
		}
		a.SecretKey = key
	} else {
		log.Printf("警告：未设置 %s，正在用开发密钥加密组织凭据；生产环境必须设置它（32 字节，base64）", directory.SecretKeyEnv)
	}
	if err := a.EnsureGlobals(ctx); err != nil {
		log.Fatalf("全局默认: %v", err)
	}
	if err := a.EnsurePlatformAdmin(ctx, env("PLATFORM_ADMIN_EMAIL", ""), env("PLATFORM_ADMIN_PASSWORD", ""), env("PLATFORM_ADMIN_NAME", "")); err != nil {
		log.Fatalf("平台管理员: %v", err)
	}
	if env("PLATFORM_ADMIN_EMAIL", "") != "" {
		log.Printf("平台管理员：%s（后台 /admin/）", env("PLATFORM_ADMIN_EMAIL", ""))
	}
	if seedDemo {
		orgID, err := a.SeedDemo(ctx)
		if err != nil {
			log.Fatalf("演示数据: %v", err)
		}
		_ = orgID
		log.Printf("演示组织就绪：登录 zhong@demo.local / demo1234（其他：li、zhang、zhao@demo.local）")
	}
	// 所有组织补齐内置任务类型、角色、能力标签（幂等）
	if orgs, err := allOrgIDs(ctx, st); err == nil {
		for _, id := range orgs {
			if err := a.EnsureOrgDefaults(ctx, id); err != nil {
				log.Fatalf("组织默认 %s: %v", id, err)
			}
		}
	}

	// 心跳超时巡检
	go func() {
		for {
			time.Sleep(time.Minute)
			orgs, err := allOrgIDs(ctx, st)
			if err != nil {
				log.Printf("巡检: %v", err)
				continue
			}
			for _, id := range orgs {
				if n, err := a.ExpireStaleRuns(ctx, id); err != nil {
					log.Printf("巡检 %s: %v", id, err)
				} else if n > 0 {
					log.Printf("组织 %s：%d 段执行记录因心跳超时结束", id, n)
				}
				if n, err := a.ExpireProposals(ctx, id); err != nil {
					log.Printf("待确认操作巡检 %s: %v", id, err)
				} else if n > 0 {
					log.Printf("组织 %s：%d 条待确认操作因超过七天没人确认而作废", id, n)
				}
				// 幂等键（ADR 0025）：过了 24 小时的键清掉，同一个键之后可以重新使用
				if n, err := a.SweepIdempotencyKeys(ctx, id); err != nil {
					log.Printf("幂等键巡检 %s: %v", id, err)
				} else if n > 0 {
					log.Printf("组织 %s：清掉 %d 个过期的幂等键", id, n)
				}
			}
			// 通知外发（ADR 0019）：刚逾期的任务、刚到期的里程碑排一次提醒（同一事项只一次）
			for _, id := range orgs {
				if err := a.EnqueueDueReminders(ctx, id, time.Now()); err != nil {
					log.Printf("逾期提醒 %s: %v", id, err)
				}
			}
			// Agent 设备码（ADR 0018）：过期的申请标为过期，旧记录清理
			if n, err := a.ExpireDeviceCodes(ctx); err != nil {
				log.Printf("设备码巡检: %v", err)
			} else if n > 0 {
				log.Printf("%d 条 Agent 接入申请因超过 15 分钟没人批准而过期", n)
			}
			// 外部目录定时同步（ADR 0017）：每小时 / 每天到点的组织跑一次
			for _, res := range a.RunScheduledDirectorySyncs(ctx, time.Now()) {
				if res.Err != nil {
					log.Printf("外部目录同步 %s: %v", res.OrgID, res.Err)
				} else if res.Run != nil {
					log.Printf("组织 %s：外部目录同步%s（新增 %d 团队 / %d 成员，停用 %d 成员）", res.OrgID, res.Run.Status, res.Run.AddedTeams, res.Run.AddedMembers, res.Run.DeactivatedMembers)
				}
			}
		}
	}()

	// 通知外发（ADR 0019）：每 15 秒把到点的投递发出去（3 次、退避、安静时段顺延；多实例时数据库建议锁保证只有一个在发）
	go func() {
		for {
			time.Sleep(15 * time.Second)
			st, err := a.RunNotificationDeliveries(ctx, time.Now())
			if err != nil {
				log.Printf("通知外发: %v", err)
			}
			if st.Sent+st.Failed+st.Skipped > 0 {
				log.Printf("通知外发：发出 %d，重试 %d，失败 %d，未发 %d", st.Sent, st.Retried, st.Failed, st.Skipped)
			}
		}
	}()

	apiSrv := &api.Server{App: a, AllowOrigins: strings.Split(env("ALLOW_ORIGINS", "http://localhost:3000"), ","), SecureCookie: env("SECURE_COOKIE", "0") == "1"}
	mux := http.NewServeMux()
	mux.Handle("/api/", apiSrv.Handler())
	mux.Handle("/mcp", mcp.Handler(a, apiSrv.Authenticate))
	// 接入链接的短地址（ADR 0024）：贴给 Agent 的就是它；浏览器打开会跳到前端的 /connect/ 页
	mux.Handle("GET /connect", apiSrv.ConnectAlias())
	mux.Handle("/", staticHandler(webDir))

	log.Printf("axiomd 监听 %s；API /api/v1，MCP /mcp，接入链接 /connect，前端目录 %s", addr, webDir)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func allOrgIDs(ctx context.Context, st *store.Store) ([]string, error) {
	rows, err := st.Pool.Query(ctx, `select id from organizations where deactivated_at is null`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// staticHandler 托管前端静态导出；找不到文件时回退到对应目录的 index.html 或根 index.html。
func staticHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		if st, err := os.Stat(p + ".html"); err == nil && !st.IsDir() {
			http.ServeFile(w, r, p+".html")
			return
		}
		if st, err := os.Stat(filepath.Join(p, "index.html")); err == nil && !st.IsDir() {
			http.ServeFile(w, r, filepath.Join(p, "index.html"))
			return
		}
		// 动态路由（/tasks/<id>/）回退到该路由的占位页 out/<路由>/_/index.html
		parent := filepath.Dir(p)
		if ph := filepath.Join(parent, "_", "index.html"); fileExists(ph) {
			http.ServeFile(w, r, ph)
			return
		}
		if entries, err := os.ReadDir(parent); err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "[") && strings.HasSuffix(e.Name(), "].html") {
					http.ServeFile(w, r, filepath.Join(parent, e.Name()))
					return
				}
				if e.IsDir() && strings.HasPrefix(e.Name(), "[") {
					if _, err := os.Stat(filepath.Join(parent, e.Name(), "index.html")); err == nil {
						http.ServeFile(w, r, filepath.Join(parent, e.Name(), "index.html"))
						return
					}
				}
			}
		}
		index := filepath.Join(dir, "index.html")
		if _, err := os.Stat(index); err == nil {
			http.ServeFile(w, r, index)
			return
		}
		http.Error(w, "前端尚未构建：在 web 目录执行 pnpm build", http.StatusNotFound)
	})
}
