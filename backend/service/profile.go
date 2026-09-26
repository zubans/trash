package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// ProfileService — то, что пользователь делает со своей учёткой сам: профиль
// и сохранённые адреса подачи. Раньше это жило в AdminService и вызывалось из
// админского обработчика с user.ID вызывающего — сервис администратора,
// который обслуживает заказчика, путал и права, и ответственность.
type ProfileService struct {
	users     repository.UserRepository
	addresses repository.AddressRepository
	// settings — публичные системные настройки (валюта, интервал отправки
	// позиции). Читаются здесь же: экран настроек приложения — часть профиля,
	// а не панели.
	settings settingsGetter
}

// NewProfileService создаёт ProfileService. addresses может быть nil — тогда
// адресные операции отвечают ErrNotConfigured.
func NewProfileService(users repository.UserRepository, addresses repository.AddressRepository) *ProfileService {
	return &ProfileService{users: users, addresses: addresses}
}

// WithSettings подключает системные настройки для публичного экрана настроек.
func (s *ProfileService) WithSettings(settings settingsGetter) *ProfileService {
	s.settings = settings
	return s
}

// PublicSettings отдаёт настройки целиком; обработчик выбирает из них те, что
// показывают всем. Без хранилища — пустая карта: приложение возьмёт умолчания.
func (s *ProfileService) PublicSettings(ctx context.Context) (map[string]string, error) {
	if s.settings == nil {
		return map[string]string{}, nil
	}
	return s.settings.GetSettings(ctx)
}

// GetProfile возвращает профиль аутентифицированного пользователя, включая адрес заказчика.
func (s *ProfileService) GetProfile(ctx context.Context, userID uuid.UUID) (map[string]interface{}, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, userNotFound(err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	user.Password = ""

	profile := map[string]interface{}{
		"id":         user.ID,
		"role":       user.Role,
		"roles":      user.Roles,
		"phone":      user.Phone,
		"email":      user.Email,
		"balance":    user.Balance,
		"status":     user.Status,
		"created_at": user.CreatedAt,
		"first_name": user.FirstName,
		"last_name":  user.LastName,
		"patronymic": user.Patronymic,
		"birth_date": user.BirthDateString(),
		"age":        user.GetAge(),
		"address":    "",
	}

	profile["addresses"] = []repository.Address{}
	if s.addresses != nil {
		addresses, err := s.addresses.List(ctx, userID)
		if err != nil {
			log.Printf("[GetProfile] failed to load addresses for %s: %v", userID, err)
		} else {
			profile["addresses"] = addresses
			for _, a := range addresses {
				if a.IsDefault {
					profile["default_address"] = a.Address
					profile["address"] = a.Address
					break
				}
			}
		}
	}
	if _, ok := profile["default_address"]; !ok {
		profile["default_address"] = profile["address"]
	}

	return profile, nil
}

func (s *ProfileService) requireAddresses() error {
	if s.addresses == nil {
		return fmt.Errorf("%w: address storage", ErrNotConfigured)
	}
	return nil
}

// ListAddresses возвращает сохранённые адреса подачи пользователя.
func (s *ProfileService) ListAddresses(ctx context.Context, userID uuid.UUID) ([]repository.Address, error) {
	if err := s.requireAddresses(); err != nil {
		return nil, err
	}
	return s.addresses.List(ctx, userID)
}

// AddAddress сохраняет новый адрес подачи.
func (s *ProfileService) AddAddress(ctx context.Context, userID uuid.UUID, address Address) ([]repository.Address, error) {
	if err := s.requireAddresses(); err != nil {
		return nil, err
	}
	if err := address.Validate(); err != nil {
		return nil, validationError(err.Error())
	}
	return s.addresses.Add(ctx, nil, userID, address.ToRecord())
}

// DeleteAddress удаляет один из адресов пользователя. ref — id адреса; номер
// строки тоже принимается, потому что установленное приложение присылает
// позицию в списке, а не id. Разрешение позиции — здесь, а не в обработчике:
// это правило о том, как клиенты называют адрес, а не о том, как читать URL.
func (s *ProfileService) DeleteAddress(ctx context.Context, userID uuid.UUID, ref string) ([]repository.Address, error) {
	if err := s.requireAddresses(); err != nil {
		return nil, err
	}
	addressID, err := uuid.Parse(ref)
	if err != nil {
		index, convErr := strconv.Atoi(ref)
		if convErr != nil {
			return nil, validationError("invalid address id")
		}
		current, err := s.addresses.List(ctx, userID)
		if err != nil {
			return nil, err
		}
		if index < 0 || index >= len(current) {
			return nil, repository.ErrAddressNotFound
		}
		addressID = current[index].ID
	}
	return s.addresses.Delete(ctx, userID, addressID)
}

// SetDefaultAddress отмечает, с какого адреса должны начинаться новые заказы.
func (s *ProfileService) SetDefaultAddress(ctx context.Context, userID, addressID uuid.UUID) ([]repository.Address, error) {
	if err := s.requireAddresses(); err != nil {
		return nil, err
	}
	return s.addresses.SetDefault(ctx, userID, addressID)
}

// SetDefaultAddressByValue — то же для клиентов, опознающих адрес по его тексту.
func (s *ProfileService) SetDefaultAddressByValue(ctx context.Context, userID uuid.UUID, address string) ([]repository.Address, error) {
	if err := s.requireAddresses(); err != nil {
		return nil, err
	}
	return s.addresses.SetDefaultByValue(ctx, userID, strings.TrimSpace(address))
}
