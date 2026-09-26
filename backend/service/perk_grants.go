package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// PerkGrants — привилегии вне покупки: ручная выдача и отзыв (право
// shop_orders.edit) и напоминания о конце очереди.
type PerkGrants struct {
	perks  repository.PerkRepository
	rules  *PerkRules
	ledger *Ledger
	mail   repository.MailRepository
	now    func() time.Time
}

// NewPerkGrants собирает выдачу привилегий.
func NewPerkGrants(perks repository.PerkRepository, rules *PerkRules, ledger *Ledger) *PerkGrants {
	return &PerkGrants{perks: perks, rules: rules, ledger: ledger, now: time.Now}
}

// WithMail подключает внутреннюю почту: письмо о выданной привилегии и
// напоминание о конце.
func (s *PerkGrants) WithMail(mail repository.MailRepository) *PerkGrants {
	s.mail = mail
	return s
}

// GrantPerkRequest — ручная выдача привилегии: компенсация, акция.
type GrantPerkRequest struct {
	Rule   string                 `json:"rule"`
	Config map[string]interface{} `json:"config"`
	Days   int                    `json:"days"`
	Reason string                 `json:"reason"`
}

// GrantPerk выдаёт привилегию без денег. Она встаёт в ту же общую очередь,
// что и купленные: беспроцентный день, выданный поверх множителя, не съедает
// его оставшиеся дни.
func (s *PerkGrants) GrantPerk(ctx context.Context, adminID, userID uuid.UUID, req GrantPerkRequest) (*repository.UserPerk, error) {
	fields := map[string]string{}
	sellable, err := s.rules.Sellable(ctx, req.Rule, req.Config)
	if errors.Is(err, ErrInvalidPerk) {
		fields["rule"] = strings.TrimPrefix(err.Error(), ErrInvalidPerk.Error()+": ")
	} else if err != nil {
		return nil, err
	}
	if req.Days <= 0 {
		fields["days"] = "Срок обязателен и больше нуля"
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		fields["reason"] = "Укажите причину"
	}
	if len(fields) > 0 {
		return nil, shopValidation(fields)
	}
	perk := &repository.UserPerk{UserID: userID, RuleCode: sellable.RuleCode, RuleVersionID: sellable.VersionID,
		Config: sellable.Config, GrantedBy: &adminID, Reason: &req.Reason}
	err = s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := s.perks.LockQueue(ctx, tx, userID); err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrNotFound) {
				return shopNotFound()
			}
			return err
		}
		start, err := s.perks.NextStart(ctx, tx, userID, s.now())
		if err != nil {
			return err
		}
		perk.StartsAt, perk.ExpiresAt = start, start.AddDate(0, 0, req.Days)
		if err := s.perks.Create(ctx, tx, perk); err != nil {
			return err
		}
		if s.mail != nil {
			if err := s.mail.Send(ctx, tx, &repository.Mail{
				UserID: userID, Kind: repository.MailKindShop, Subject: "Вам выдана привилегия",
				Body: fmt.Sprintf("Привилегия на комиссию действует с %s по %s.",
					perk.StartsAt.Format("02.01.2006"), perk.ExpiresAt.Format("02.01.2006")),
				RefType: "perk", RefID: perk.ID.String(),
			}); err != nil {
				log.Printf("[shop] cannot mail user %s about a granted perk: %v", userID, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %s granted perk %s (%s, %d days) to user %s: %s", adminID, perk.ID, perk.RuleCode, req.Days, userID, req.Reason)
	return perk, nil
}

// RevokePerk отзывает привилегию без возврата денег. Купленную с возвратом
// отменяют через покупку.
func (s *PerkGrants) RevokePerk(ctx context.Context, adminID, perkID uuid.UUID) (*repository.UserPerk, error) {
	perk, err := s.perks.Revoke(ctx, nil, perkID, adminID)
	if errors.Is(err, repository.ErrPerkNotFound) {
		return nil, shopNotFound()
	}
	if err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %s revoked perk %s of user %s without a refund", adminID, perkID, perk.UserID)
	return perk, nil
}

// perkReminderLead — за сколько до конца последней привилегии в очереди
// приходит письмо (implementation_plan_shop.md §3.6).
const perkReminderLead = 3 * 24 * time.Hour

// SendPerkReminders пишет владельцам привилегий, которые закончатся в
// ближайшие три дня и за которыми в очереди ничего нет. Отметка ставится тем
// же оператором, что и письмо, в одной транзакции: второй проход, даже на
// другом процессе, второго письма не пошлёт.
func (s *PerkGrants) SendPerkReminders(ctx context.Context) (int, error) {
	if s.mail == nil {
		return 0, nil
	}
	now := s.now()
	due, err := s.perks.DueReminders(ctx, now, now.Add(perkReminderLead), 200)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, perk := range due {
		err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
			claimed, err := s.perks.MarkReminded(ctx, tx, perk.ID)
			if err != nil || !claimed {
				return err
			}
			sent++
			return s.mail.Send(ctx, tx, &repository.Mail{
				UserID: perk.UserID, Kind: repository.MailKindShop,
				Subject: "Привилегия скоро закончится",
				Body: fmt.Sprintf("Сниженная комиссия действует до %s. Продлить её можно в магазине — новая начнётся сразу после текущей.",
					perk.ExpiresAt.Format("02.01.2006 15:04")),
				RefType: "perk", RefID: perk.ID.String(),
			})
		})
		if err != nil {
			return sent, err
		}
	}
	return sent, nil
}
