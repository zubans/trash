package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Происхождение правила привилегии.
const (
	// PerkRuleShipped — правило из бинарника (backend/perks): текст меняет
	// только релиз.
	PerkRuleShipped = "SHIPPED"
	// PerkRuleOwn — правило, написанное в админке; его текст — в версиях.
	PerkRuleOwn = "OWN"
)

// ErrPerkRuleNotFound — правила с таким кодом нет.
var ErrPerkRuleNotFound = errors.New("perk rule not found")

// PerkRule — строка справочника правил.
type PerkRule struct {
	Code      string    `json:"code"`
	Title     string    `json:"title"`
	Origin    string    `json:"origin"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PerkRuleVersion — неизменяемый текст собственного правила.
type PerkRuleVersion struct {
	ID        uuid.UUID  `json:"id"`
	RuleCode  string     `json:"rule_code"`
	Hash      string     `json:"hash"`
	Source    string     `json:"source"`
	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// PerkRuleRepository хранит правила привилегий и версии собственных правил.
type PerkRuleRepository interface {
	List(ctx context.Context) ([]*PerkRule, error)
	Get(ctx context.Context, code string) (*PerkRule, error)
	// Save заводит правило или меняет его название и активность. Происхождение
	// у существующего правила не меняется.
	Save(ctx context.Context, rule *PerkRule) error
	// AddVersion пишет новую версию. Повтор текущего текста отсекает сервис.
	AddVersion(ctx context.Context, v *PerkRuleVersion) (*PerkRuleVersion, error)
	// LatestVersion — текущая версия собственного правила; ErrPerkRuleNotFound,
	// если версий нет.
	LatestVersion(ctx context.Context, code string) (*PerkRuleVersion, error)
	GetVersion(ctx context.Context, id uuid.UUID) (*PerkRuleVersion, error)
}

type perkRuleRepo struct {
	db *sql.DB
}

// NewPerkRuleRepository создаёт PerkRuleRepository.
func NewPerkRuleRepository(db *sql.DB) PerkRuleRepository {
	return &perkRuleRepo{db: db}
}

const perkRuleColumns = `code, title, origin, is_active, created_at, updated_at`

func scanPerkRule(row rowScanner) (*PerkRule, error) {
	var r PerkRule
	if err := row.Scan(&r.Code, &r.Title, &r.Origin, &r.IsActive, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func (r *perkRuleRepo) List(ctx context.Context) ([]*PerkRule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+perkRuleColumns+` FROM perk_rules ORDER BY origin DESC, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*PerkRule, 0)
	for rows.Next() {
		rule, err := scanPerkRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

func (r *perkRuleRepo) Get(ctx context.Context, code string) (*PerkRule, error) {
	rule, err := scanPerkRule(r.db.QueryRowContext(ctx, `SELECT `+perkRuleColumns+` FROM perk_rules WHERE code = $1`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPerkRuleNotFound
	}
	return rule, err
}

func (r *perkRuleRepo) Save(ctx context.Context, rule *PerkRule) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO perk_rules (code, title, origin, is_active)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (code) DO UPDATE SET
			title = EXCLUDED.title, is_active = EXCLUDED.is_active, updated_at = now()
		RETURNING origin, created_at, updated_at`,
		rule.Code, rule.Title, rule.Origin, rule.IsActive).Scan(&rule.Origin, &rule.CreatedAt, &rule.UpdatedAt)
}

const perkVersionColumns = `id, rule_code, hash, source, created_by, created_at`

func scanPerkVersion(row rowScanner) (*PerkRuleVersion, error) {
	var v PerkRuleVersion
	err := row.Scan(&v.ID, &v.RuleCode, &v.Hash, &v.Source, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPerkRuleNotFound
	}
	return &v, err
}

func (r *perkRuleRepo) AddVersion(ctx context.Context, v *PerkRuleVersion) (*PerkRuleVersion, error) {
	return scanPerkVersion(r.db.QueryRowContext(ctx, `
		INSERT INTO perk_rule_versions (rule_code, hash, source, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING `+perkVersionColumns,
		v.RuleCode, v.Hash, v.Source, v.CreatedBy))
}

func (r *perkRuleRepo) LatestVersion(ctx context.Context, code string) (*PerkRuleVersion, error) {
	return scanPerkVersion(r.db.QueryRowContext(ctx,
		`SELECT `+perkVersionColumns+` FROM perk_rule_versions WHERE rule_code = $1 ORDER BY created_at DESC, id LIMIT 1`, code))
}

func (r *perkRuleRepo) GetVersion(ctx context.Context, id uuid.UUID) (*PerkRuleVersion, error) {
	return scanPerkVersion(r.db.QueryRowContext(ctx,
		`SELECT `+perkVersionColumns+` FROM perk_rule_versions WHERE id = $1`, id))
}
