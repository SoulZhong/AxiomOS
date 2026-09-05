package app

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/teemo/axiomos/internal/domain"
	"github.com/teemo/axiomos/internal/i18n"
	"github.com/teemo/axiomos/internal/store"
)

// AdminSession 是平台管理员会话。
type AdminSession struct {
	Account *domain.Account
	Locale  i18n.Locale
}

func checkPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// AdminLogin 平台管理员登录。
func (a *App) AdminLogin(ctx context.Context, email, password string) (string, *AdminSession, error) {
	acc, hash, err := a.Store.AccountByEmail(ctx, a.Store.Pool, strings.TrimSpace(email))
	if err != nil || checkPassword(hash, password) != nil {
		return "", nil, Bad("err.bad_credentials")
	}
	if !acc.PlatformAdmin {
		return "", nil, Forbidden("err.admin_required")
	}
	tok := store.NewID("adm") + store.NewID("")[1:]
	if err := a.Store.CreateSession(ctx, a.Store.Pool, tok, acc.ID, "", sessionTTL); err != nil {
		return "", nil, err
	}
	return tok, &AdminSession{Account: acc, Locale: i18n.Normalize(acc.Locale)}, nil
}

// AdminSessionFromToken 解析平台管理员会话。
func (a *App) AdminSessionFromToken(ctx context.Context, token string) (*AdminSession, error) {
	accountID, orgID, err := a.Store.SessionLookup(ctx, a.Store.Pool, token)
	if err != nil || orgID != "" {
		return nil, Unauthorized("err.admin_required")
	}
	acc, err := a.Store.AccountByID(ctx, a.Store.Pool, accountID)
	if err != nil || !acc.PlatformAdmin {
		return nil, Unauthorized("err.admin_required")
	}
	return &AdminSession{Account: acc, Locale: i18n.Normalize(acc.Locale)}, nil
}

// OrgOverview 是平台后台里的组织概览。
type OrgOverview struct {
	*domain.Organization
	Owner *struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"owner"`
	MemberCount int     `json:"member_count"`
	AgentCount  int     `json:"agent_count"`
	TaskCount   int     `json:"task_count"`
	Cost30d     float64 `json:"cost_30d"`
}

func (a *App) orgOverview(ctx context.Context, org *domain.Organization) (*OrgOverview, error) {
	v := &OrgOverview{Organization: org}
	err := a.Store.WithOrg(ctx, org.ID, func(tx pgx.Tx) error {
		c, err := a.Store.OrgCounts(ctx, tx, org.ID)
		if err != nil {
			return err
		}
		v.MemberCount, v.AgentCount, v.TaskCount, v.Cost30d = c.Members, c.Agents, c.Tasks, c.Cost30d
		if org.OwnerMemberID != "" {
			if m, err := a.Store.MemberByID(ctx, tx, org.OwnerMemberID); err == nil {
				o := &struct {
					ID    string `json:"id"`
					Name  string `json:"name"`
					Email string `json:"email"`
				}{ID: m.ID, Name: m.Name}
				if acc, err := a.Store.AccountByID(ctx, tx, m.AccountID); err == nil {
					o.Email = acc.Email
				}
				v.Owner = o
			}
		}
		return nil
	})
	return v, err
}

// AdminListOrganizations 列出全部组织。
func (a *App) AdminListOrganizations(ctx context.Context) ([]*OrgOverview, error) {
	orgs, err := a.Store.ListOrganizations(ctx, a.Store.Pool)
	if err != nil {
		return nil, err
	}
	out := []*OrgOverview{}
	for _, o := range orgs {
		v, err := a.orgOverview(ctx, o)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// AdminGetOrganization 单个组织。
func (a *App) AdminGetOrganization(ctx context.Context, id string) (*OrgOverview, error) {
	org, err := a.Store.OrganizationByID(ctx, a.Store.Pool, id)
	if err != nil {
		return nil, wrapErr(err)
	}
	return a.orgOverview(ctx, org)
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)

// CreateOrgInput 是创建组织的输入。
type CreateOrgInput struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Currency      string `json:"currency"`
	DefaultLocale string `json:"default_locale"`
	OwnerEmail    string `json:"owner_email"`
	OwnerName     string `json:"owner_name"`
}

// CreatedOrg 是创建结果。
type CreatedOrg struct {
	*OrgOverview
	OwnerInviteURL string `json:"owner_invite_url,omitempty"`
}

// AdminCreateOrganization 创建组织并指定负责人。负责人账号不存在时生成邀请链接。
func (a *App) AdminCreateOrganization(ctx context.Context, in CreateOrgInput) (*CreatedOrg, error) {
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugRe.MatchString(in.Slug) {
		return nil, Bad("err.slug")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, Bad("err.title_required")
	}
	in.OwnerEmail = strings.ToLower(strings.TrimSpace(in.OwnerEmail))
	if !emailRe.MatchString(in.OwnerEmail) {
		return nil, Bad("err.email_required")
	}
	if _, err := a.Store.OrganizationBySlug(ctx, a.Store.Pool, in.Slug); err == nil {
		return nil, Bad("err.slug_taken", in.Slug)
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	loc := i18n.Normalize(in.DefaultLocale)
	if loc == "" {
		loc = i18n.Default
	}
	org, err := a.Store.CreateOrganization(ctx, a.Store.Pool, in.Slug, strings.TrimSpace(in.Name), strings.ToUpper(in.Currency))
	if err != nil {
		return nil, err
	}
	org.DefaultLocale = string(loc)
	if err := a.Store.UpdateOrganization(ctx, a.Store.Pool, org); err != nil {
		return nil, err
	}
	if err := a.EnsureOrgDefaults(ctx, org.ID); err != nil {
		return nil, err
	}
	ownerName := strings.TrimSpace(in.OwnerName)
	if ownerName == "" {
		ownerName = strings.Split(in.OwnerEmail, "@")[0]
	}
	inviteURL := ""
	err = a.Store.WithOrg(ctx, org.ID, func(tx pgx.Tx) error {
		acc, _, err := a.Store.AccountByEmail(ctx, tx, in.OwnerEmail)
		if err == store.ErrNotFound {
			// 先建成员占位需要账号：走邀请，接受后成为负责人
			inv := &domain.Invitation{OrgID: org.ID, Email: in.OwnerEmail, Name: ownerName, Roles: []string{"admin"}, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}
			token, err := a.Store.CreateInvitation(ctx, tx, inv)
			if err != nil {
				return err
			}
			inviteURL = a.PublicURL + "/invite/" + token + "/"
			// 记下：接受邀请后要成为负责人（用 name 字段标记）
			return nil
		} else if err != nil {
			return err
		}
		m, err := a.Store.CreateMember(ctx, tx, org.ID, acc.ID, ownerName, []string{"admin"})
		if err != nil {
			return err
		}
		return a.Store.SetOrganizationOwner(ctx, tx, org.ID, m.ID)
	})
	if err != nil {
		return nil, err
	}
	v, err := a.AdminGetOrganization(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	return &CreatedOrg{OrgOverview: v, OwnerInviteURL: inviteURL}, nil
}

// OrgAdminPatch 是平台可改字段。
type OrgAdminPatch struct {
	Name        *string `json:"name"`
	Deactivated *bool   `json:"deactivated"`
}

// AdminUpdateOrganization 修改组织名或停用/恢复。
func (a *App) AdminUpdateOrganization(ctx context.Context, id string, in OrgAdminPatch) (*OrgOverview, error) {
	org, err := a.Store.OrganizationByID(ctx, a.Store.Pool, id)
	if err != nil {
		return nil, wrapErr(err)
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		org.Name = strings.TrimSpace(*in.Name)
	}
	if in.Deactivated != nil {
		if *in.Deactivated {
			now := time.Now()
			org.DeactivatedAt = &now
		} else {
			org.DeactivatedAt = nil
		}
	}
	if err := a.Store.UpdateOrganization(ctx, a.Store.Pool, org); err != nil {
		return nil, err
	}
	return a.orgOverview(ctx, org)
}

// AdminGlobalPrices 全局价格表。
func (a *App) AdminGlobalPrices(ctx context.Context) ([]domain.Price, error) {
	rows, err := a.Store.ListPrices(ctx, a.Store.Pool, "")
	if err != nil {
		return nil, err
	}
	out := []domain.Price{}
	for _, r := range rows {
		if r.OrgID == "" {
			out = append(out, r.Price)
		}
	}
	return out, nil
}

// AdminSetGlobalPrice 新增或修改全局价格。
func (a *App) AdminSetGlobalPrice(ctx context.Context, p domain.Price) error {
	if !validPrice(p) || p.ModelID == "" {
		return Bad("err.price_fields")
	}
	if p.Currency == "" {
		p.Currency = "USD"
	}
	return a.Store.UpsertPrice(ctx, a.Store.Pool, "", p)
}

// AdminDeleteGlobalPrice 删除全局价格。
func (a *App) AdminDeleteGlobalPrice(ctx context.Context, modelID string) error {
	return a.Store.DeletePrice(ctx, a.Store.Pool, "", modelID)
}

// AdminStats 平台概览。
type AdminStats struct {
	Organizations int            `json:"organizations"`
	Members       int            `json:"members"`
	Agents        int            `json:"agents"`
	Runs30d       int            `json:"runs_30d"`
	Cost30d       []AdminOrgCost `json:"cost_30d"`
}

// AdminOrgCost 是某组织 30 天成本。
type AdminOrgCost struct {
	OrgID    string  `json:"org_id"`
	Name     string  `json:"name"`
	Cost     float64 `json:"cost"`
	Currency string  `json:"currency"`
}

// AdminStatsView 汇总全部组织。
func (a *App) AdminStatsView(ctx context.Context) (*AdminStats, error) {
	orgs, err := a.Store.ListOrganizations(ctx, a.Store.Pool)
	if err != nil {
		return nil, err
	}
	s := &AdminStats{Organizations: len(orgs), Cost30d: []AdminOrgCost{}}
	for _, o := range orgs {
		err := a.Store.WithOrg(ctx, o.ID, func(tx pgx.Tx) error {
			c, err := a.Store.OrgCounts(ctx, tx, o.ID)
			if err != nil {
				return err
			}
			s.Members += c.Members
			s.Agents += c.Agents
			s.Cost30d = append(s.Cost30d, AdminOrgCost{OrgID: o.ID, Name: o.Name, Cost: c.Cost30d, Currency: o.Currency})
			n, err := a.Store.RunsLast30d(ctx, tx)
			if err != nil {
				return err
			}
			s.Runs30d += n
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// AdminListAdmins 列出平台管理员。
func (a *App) AdminListAdmins(ctx context.Context) ([]*domain.Account, error) {
	out, err := a.Store.ListPlatformAdmins(ctx, a.Store.Pool)
	if out == nil {
		out = []*domain.Account{}
	}
	return out, err
}

// AdminAddAdmin 新增平台管理员（账号已存在则提升）。
func (a *App) AdminAddAdmin(ctx context.Context, email, name, password string) (*domain.Account, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailRe.MatchString(email) {
		return nil, Bad("err.email_required")
	}
	acc, _, err := a.Store.AccountByEmail(ctx, a.Store.Pool, email)
	if err == store.ErrNotFound {
		if len(password) < 8 {
			return nil, Bad("err.password_short")
		}
		hash, err := HashPassword(password)
		if err != nil {
			return nil, err
		}
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
		acc, err = a.Store.CreateAccount(ctx, a.Store.Pool, email, hash, name)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err := a.Store.SetPlatformAdmin(ctx, a.Store.Pool, acc.ID, true); err != nil {
		return nil, err
	}
	acc.PlatformAdmin = true
	return acc, nil
}
