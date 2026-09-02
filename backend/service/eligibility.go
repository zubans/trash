package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"healthlogin/backend/repository"
)

// Бизнес-умолчания. Каждое можно переопределить через system_settings, чтобы
// правила не были закопаны в коде магическими числами.
const (
	defaultMaxActiveOrders        = 3
	defaultMaxExecutedUnconfirmed = 6
	defaultRejectPenaltyShare     = 0.5
	defaultMinBalanceLimit        = 0.0
)

// ErrExecutorNotEligible сообщает, что исполнитель не может взять конкретный заказ.
var ErrExecutorNotEligible = errors.New("executor is not eligible for this order")

// ErrCustomerNotEligible сообщает, что заказчик не может заказать конкретный вариант услуги.
var ErrCustomerNotEligible = errors.New("customer is not eligible for this service")

// canCustomerOrderVariant — единственное место, решающее, может ли заказчик
// разместить заказ по варианту услуги. Вариант с флагом
// requires_verification может заказать только вручную верифицированный
// заказчик — зеркало canExecutorTakeOrder на стороне исполнителя. Он
// используется и при фильтрации каталога, и при собственно создании заказа,
// поэтому проверку нельзя обойти, отправив напрямую известный id варианта.
// Возраст (min_age) намеренно проверяется только на стороне исполнителя: он
// ограничивает, кто может выполнять работу, а не кто может её попросить.
//
// Вариант, управляемый скриптом поведения, получает хук can_order этого скрипта
// поверх этих правил, но никогда вместо них: скрипт может ограничить, кто
// заказывает услугу, но не может выдать освобождение от бана.
func canCustomerOrderVariant(ctx context.Context, behaviors *Behaviors, customer *repository.User, variant *repository.ServiceNode) error {
	if customer == nil {
		return ErrCustomerNotEligible
	}
	if customer.Status == "BANNED" {
		return errors.New("аккаунт заблокирован")
	}
	if variant == nil {
		return nil
	}
	if variant.RequiresVerification && !customer.IsVerified() {
		return errors.New("для этой услуги требуется подтверждённый аккаунт")
	}
	return behaviors.CanOrder(ctx, customer, variant)
}

// formatGeo отдаёт пару координат в форме «lat,lon», используемой в
// customer_profiles.last_geo. Воркер подбора разбирает эту колонку как
// координаты, поэтому писать туда что-либо иное нельзя.
func formatGeo(lat, lon float64) string {
	return fmt.Sprintf("%f,%f", lat, lon)
}

// settingsGetter — небольшой срез SettingsRepository, используемый для настраиваемых величин.
type settingsGetter interface {
	GetSettings(ctx context.Context) (map[string]string, error)
}

func settingFloat(ctx context.Context, repo settingsGetter, key string, defaultValue float64) float64 {
	if repo == nil {
		return defaultValue
	}
	settings, err := repo.GetSettings(ctx)
	if err != nil {
		return defaultValue
	}
	if v, ok := settings[key]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func settingInt(ctx context.Context, repo settingsGetter, key string, defaultValue int) int {
	return int(settingFloat(ctx, repo, key, float64(defaultValue)))
}

// canExecutorTakeOrder — единственное место, решающее, позволено ли
// исполнителю работать по данному варианту услуги. Он используется и при
// фильтрации списков заказов, и когда исполнитель реально действует по заказу,
// поэтому ограничения нельзя обойти, вызвав эндпоинт напрямую с известным id
// заказа.
func canExecutorTakeOrder(executor *repository.User, variant *repository.ServiceNode) error {
	if executor == nil {
		return ErrExecutorNotEligible
	}
	if executor.Status == "BANNED" {
		return errors.New("аккаунт заблокирован")
	}
	if variant == nil {
		return nil
	}
	if variant.RequiresVerification && !executor.IsVerified() {
		return errors.New("для этого заказа требуется подтверждённый аккаунт")
	}
	if variant.MinAge > 0 && executor.GetAge() < variant.MinAge {
		return fmt.Errorf("для этого заказа требуется возраст не менее %d лет", variant.MinAge)
	}
	return nil
}

// canViewOrTakeOrder — единственный предикат, решающий, может ли смотрящий
// (исполнитель и/или модератор) и ВИДЕТЬ, и ПРИНЯТЬ данный заказ. Через него
// идут списки заказов (карта и таблица) и путь принятия, поэтому то, с чем
// исполнитель может действовать, никогда не расходится с показанным ему.
//
// Правила:
//   - Услуга только для модераторов: видеть и брать заказ может только
//     MODERATOR; обычные проверки исполнителя не применяются (это доверенный персонал).
//   - Скриптовая услуга: что скажет хук can_view_or_take её поведения, поверх
//     правил ниже.
//   - Обычная услуга: применяются проверки исполнителя (бан,
//     requires_verification, min_age), а поверх них — сегментация по
//   - верификации заказчика: заказ неверифицированного заказчика виден всем
//     (именно это позволяет неверифицированному исполнителю работать с их пулом);
//   - заказ верифицированного заказчика виден только верифицированному
//     исполнителю или модератору.
func canViewOrTakeOrder(ctx context.Context, behaviors *Behaviors, viewer *repository.User, customer *repository.User, variant *repository.ServiceNode) error {
	if viewer == nil {
		return ErrExecutorNotEligible
	}
	if variant != nil && variant.ModeratorOnly {
		if !viewer.HasRole(repository.RoleModerator) {
			return ErrExecutorNotEligible
		}
		if viewer.Status == "BANNED" {
			return errors.New("аккаунт заблокирован")
		}
		return behaviors.CanViewOrTake(ctx, viewer, customer, variant)
	}
	if err := canExecutorTakeOrder(viewer, variant); err != nil {
		return err
	}
	if customer != nil && customer.IsVerified() {
		if !viewer.IsVerified() && !viewer.HasRole(repository.RoleModerator) {
			return ErrExecutorNotEligible
		}
	}
	// Скрипт выполняется последним и может только сузить разрешённое встроенными правилами.
	// Услуга верификации пользуется этим, чтобы допускать одних модераторов.
	return behaviors.CanViewOrTake(ctx, viewer, customer, variant)
}
