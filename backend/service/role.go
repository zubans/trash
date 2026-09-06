package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// roleCodePattern ограничивает код роли тем, что можно без оговорок положить в
// users.role, в JWT-независимую проверку прав и в фильтр списка пользователей:
// заглавные латинские буквы, цифры и подчёркивание.
var roleCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,31}$`)

// RoleService — администрирование справочника ролей: сами роли, их права и то,
// кому они подключены.
//
// Две вещи он охраняет всегда, потому что обе способны запереть платформу
// снаружи. Первая — администратор: последнего нельзя лишить роли, и себя нельзя
// разжаловать самому. Вторая — системные роли: на CUSTOMER, EXECUTOR, MODERATOR
// и ADMIN опираются маршруты и выбор дашборда, поэтому удалить их нельзя, как
// бы ни выглядела страница ролей.
type RoleService struct {
	roles       repository.RoleRepository
	users       repository.UserRepository
	admins      repository.AdminRepository
	permissions *Permissions
	sessions    SessionRevoker
}

// NewRoleService создаёт RoleService.
func NewRoleService(
	roles repository.RoleRepository,
	users repository.UserRepository,
	admins repository.AdminRepository,
	permissions *Permissions,
) *RoleService {
	return &RoleService{roles: roles, users: users, admins: admins, permissions: permissions}
}

// WithSessions подключает завершение сессий. Изменение прав должно доходить до
// уже открытого браузера, а не ждать истечения токена.
func (s *RoleService) WithSessions(sessions SessionRevoker) *RoleService {
	s.sessions = sessions
	return s
}

// List возвращает роли с правами и числом носителей.
func (s *RoleService) List(ctx context.Context) ([]*repository.Role, error) {
	roles, err := s.roles.List(ctx)
	if err != nil {
		return nil, err
	}
	// У администратора прав в базе нет: он суперпользователь по коду. Список
	// показывает ему полный каталог, иначе роль с наибольшими правами выглядела
	// бы в интерфейсе самой ограниченной.
	for _, role := range roles {
		if role.Code == repository.RoleAdmin {
			role.Permissions = AllPermissions()
		}
	}
	return roles, nil
}

// Get возвращает одну роль.
func (s *RoleService) Get(ctx context.Context, code string) (*repository.Role, error) {
	role, err := s.roles.Get(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return nil, err
	}
	if role.Code == repository.RoleAdmin {
		role.Permissions = AllPermissions()
	}
	return role, nil
}

// Create заводит роль и сразу выдаёт ей выбранные права.
func (s *RoleService) Create(ctx context.Context, actorID uuid.UUID, code, name, description string, permissions []string) (*repository.Role, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if !roleCodePattern.MatchString(code) {
		return nil, errors.New("код роли: заглавные латинские буквы, цифры и подчёркивание, от 2 до 32 символов")
	}
	if name == "" {
		return nil, errors.New("название роли обязательно")
	}
	clean, err := cleanPermissions(permissions)
	if err != nil {
		return nil, err
	}

	if err := s.roles.Create(ctx, &repository.Role{Code: code, Name: name, Description: strings.TrimSpace(description)}); err != nil {
		return nil, err
	}
	if err := s.roles.SetPermissions(ctx, code, clean); err != nil {
		return nil, err
	}
	s.permissions.Invalidate()
	log.Printf("[AUDIT] admin %s created role %s with permissions %v", actorID, code, clean)
	return s.Get(ctx, code)
}

// Update меняет название, описание и набор прав роли.
func (s *RoleService) Update(ctx context.Context, actorID uuid.UUID, code, name, description string, permissions []string) (*repository.Role, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("название роли обязательно")
	}
	current, err := s.roles.Get(ctx, code)
	if err != nil {
		return nil, err
	}
	clean, err := cleanPermissions(permissions)
	if err != nil {
		return nil, err
	}

	if err := s.roles.Update(ctx, code, name, strings.TrimSpace(description)); err != nil {
		return nil, err
	}
	// Права администратора не хранятся и не редактируются: они и так полные, а
	// сохранённый урезанный набор создал бы вид, будто их можно отнять.
	if code != repository.RoleAdmin {
		if err := s.roles.SetPermissions(ctx, code, clean); err != nil {
			return nil, err
		}
		s.permissions.Invalidate()
		s.revokeHolders(ctx, code, "role permissions change")
	}
	log.Printf("[AUDIT] admin %s updated role %s: permissions %v -> %v", actorID, code, current.Permissions, clean)
	return s.Get(ctx, code)
}

// Delete удаляет несистемную роль и снимает её со всех носителей.
func (s *RoleService) Delete(ctx context.Context, actorID uuid.UUID, code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	role, err := s.roles.Get(ctx, code)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return fmt.Errorf("роль «%s» системная: на неё опираются маршруты и дашборды, удалить её нельзя", role.Name)
	}

	// Носителей нужно разлогинить до удаления: после него уже не узнать, у кого
	// эта роль была.
	holders := s.holderIDs(ctx, code)
	if err := s.roles.Delete(ctx, code); err != nil {
		return err
	}
	s.permissions.Invalidate()
	for _, id := range holders {
		s.revoke(ctx, id, "role deleted")
	}
	log.Printf("[AUDIT] admin %s deleted role %s (%d holders)", actorID, code, len(holders))
	return nil
}

// ListUsers возвращает страницу носителей роли.
func (s *RoleService) ListUsers(ctx context.Context, code, search string, limit, offset int) ([]*repository.RoleUser, int, error) {
	if limit > maxAdminPageSize {
		limit = maxAdminPageSize
	}
	return s.roles.ListUsers(ctx, strings.ToUpper(strings.TrimSpace(code)), search, limit, offset)
}

// AssignUser подключает роль пользователю.
func (s *RoleService) AssignUser(ctx context.Context, actorID uuid.UUID, code string, userID uuid.UUID) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if _, err := s.roles.Get(ctx, code); err != nil {
		return err
	}
	if _, err := s.users.FindByID(ctx, userID); err != nil {
		return errors.New("пользователь не найден")
	}
	if err := s.roles.AssignUser(ctx, code, userID); err != nil {
		return err
	}
	s.revoke(ctx, userID, "role assigned")
	log.Printf("[AUDIT] admin %s assigned role %s to user %s", actorID, code, userID)
	return nil
}

// UnassignUser снимает роль с пользователя.
//
// Единственный отказ здесь — тот же, что охраняет смену ролей на карточке
// пользователя: администратора нельзя снять с самого себя, и последнего
// администратора нельзя снять вообще. Всё остальное разрешено, в том числе снять
// у человека последнюю роль: у пользователя без строк в user_roles основная роль
// переезжает на заказчика, а не исчезает.
func (s *RoleService) UnassignUser(ctx context.Context, actorID uuid.UUID, code string, userID uuid.UUID) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == repository.RoleAdmin {
		if userID == actorID {
			return errors.New("нельзя снять роль администратора с самого себя")
		}
		admins, err := s.admins.CountAdmins(ctx)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return errors.New("нельзя снять роль с последнего администратора")
		}
	}
	if err := s.roles.UnassignUser(ctx, code, userID); err != nil {
		return err
	}
	s.revoke(ctx, userID, "role unassigned")
	log.Printf("[AUDIT] admin %s removed role %s from user %s", actorID, code, userID)
	return nil
}

// holderIDs собирает id носителей роли постранично, чтобы завершить их сессии.
func (s *RoleService) holderIDs(ctx context.Context, code string) []uuid.UUID {
	var ids []uuid.UUID
	for offset := 0; ; offset += maxAdminPageSize {
		page, total, err := s.roles.ListUsers(ctx, code, "", maxAdminPageSize, offset)
		if err != nil {
			log.Printf("[roles] list holders of %s failed: %v", code, err)
			return ids
		}
		for _, u := range page {
			ids = append(ids, u.ID)
		}
		if len(page) == 0 || offset+len(page) >= total {
			return ids
		}
	}
}

// revokeHolders завершает сессии всех носителей роли: изменившийся набор прав
// должен дойти до уже открытой панели, а не ждать истечения токена.
func (s *RoleService) revokeHolders(ctx context.Context, code, reason string) {
	for _, id := range s.holderIDs(ctx, code) {
		s.revoke(ctx, id, reason)
	}
}

func (s *RoleService) revoke(ctx context.Context, userID uuid.UUID, reason string) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.RevokeAllSessions(ctx, userID); err != nil {
		log.Printf("[roles] revoke sessions of %s after %s failed: %v", userID, reason, err)
	}
}

// cleanPermissions отбрасывает дубли и отвергает коды, которых нет в каталоге.
// Сохранённое неизвестное право выглядело бы в матрице выданным, но не охраняло
// бы ни одного маршрута — тихая дыра вместо явной ошибки.
func cleanPermissions(permissions []string) ([]string, error) {
	seen := make(map[string]struct{}, len(permissions))
	clean := make([]string, 0, len(permissions))
	for _, code := range permissions {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if !IsKnownPermission(code) {
			return nil, fmt.Errorf("неизвестное право: %s", code)
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		clean = append(clean, code)
	}
	return clean, nil
}
