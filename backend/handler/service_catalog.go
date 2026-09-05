package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// ServiceCatalogHandler обслуживает публичные и админские HTTP-эндпоинты каталога услуг.
type ServiceCatalogHandler struct {
	catalogRepo repository.ServiceCatalogRepository
	// behaviors решает видимость узлов, чьи правила приходят из скрипта.
	// Необязательно: без него применяются только встроенные флаги — так каталог
	// и работал до появления поведений.
	behaviors *service.Behaviors
}

// NewServiceCatalogHandler создаёт ServiceCatalogHandler.
func NewServiceCatalogHandler(catalogRepo repository.ServiceCatalogRepository) *ServiceCatalogHandler {
	return &ServiceCatalogHandler{catalogRepo: catalogRepo}
}

// WithBehaviors подключает скрипты поведений к спискам каталога.
func (h *ServiceCatalogHandler) WithBehaviors(behaviors *service.Behaviors) *ServiceCatalogHandler {
	h.behaviors = behaviors
	return h
}

// hideVerificationOnly сообщает, является ли запрашивающий заказчиком, не
// прошедшим ручную верификацию. Таким заказчикам нельзя показывать услуги с
// флагом requires_verification — заказать их они не могут (проверка на создании
// заказа), так что показ только вводил бы в заблуждение. Исполнителей, админов
// и анонимов это не затрагивает; поле заполняет middleware OptionalAuth.
func hideVerificationOnly(r *http.Request) bool {
	user := userFromContext(r)
	return user != nil && user.Role == "CUSTOMER" && !user.IsVerified()
}

// visibleTo отбрасывает узлы, которые этот запрашивающий видеть не должен: с
// флагом requires_verification, когда он неверифицированный заказчик, и те,
// что от него скрывает скрипт поведения.
//
// Оба правила применяются здесь, за один проход, потому что узел, который
// вызывающий не может заказать, не должен и перечисляться: список, показывающий
// то, в чём откажет оформление, хуже, чем отсутствие узла в списке. Счётчики
// claim читаются один раз на запрос, а не один раз на узел.
func (h *ServiceCatalogHandler) visibleTo(r *http.Request, nodes []*repository.ServiceNode) []*repository.ServiceNode {
	hide := hideVerificationOnly(r)
	user := userFromContext(r)

	// Claim'ы читаются, только когда на этой странице их кому-то есть куда деть.
	// Каталог обычных услуг не должен получать лишний запрос на каждый вызов лишь
	// потому, что где-то существуют скриптовые услуги.
	var claims map[uuid.UUID]int
	for _, n := range nodes {
		if h.behaviors.Governs(n) {
			claims = h.behaviors.ClaimsFor(r.Context(), user)
			break
		}
	}

	out := make([]*repository.ServiceNode, 0, len(nodes))
	for _, n := range nodes {
		if hide && n.RequiresVerification {
			continue
		}
		if !h.behaviors.Visible(r.Context(), user, n, claims) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// ListRootCategories обслуживает GET /service-categories.
func (h *ServiceCatalogHandler) ListRootCategories(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.catalogRepo.GetRootCategories(r.Context(), repository.FilterActive)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.visibleTo(r, nodes))
}

// ListChildren обслуживает GET /service-categories/:id/children.
func (h *ServiceCatalogHandler) ListChildren(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid category id", http.StatusBadRequest)
		return
	}
	nodes, err := h.catalogRepo.GetChildren(r.Context(), id, repository.FilterActive)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.visibleTo(r, nodes))
}

// ListCategoryVariants обслуживает GET /service-categories/:id/variants.
func (h *ServiceCatalogHandler) ListCategoryVariants(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid category id", http.StatusBadRequest)
		return
	}
	nodes, err := h.catalogRepo.GetDescendants(r.Context(), id, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Возвращаем только активные варианты.
	variants := make([]*repository.ServiceNode, 0, len(nodes))
	for _, n := range nodes {
		if n.IsVariant() && n.IsActive {
			variants = append(variants, n)
		}
	}
	writeJSON(w, h.visibleTo(r, variants))
}

// ListVariants обслуживает GET /service-variants.
func (h *ServiceCatalogHandler) ListVariants(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.catalogRepo.GetActiveVariants(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.visibleTo(r, nodes))
}

// GetVariant обслуживает GET /service-variants/:id.
func (h *ServiceCatalogHandler) GetVariant(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid variant id", http.StatusBadRequest)
		return
	}
	variant, path, err := h.catalogRepo.GetVariantWithCategory(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "variant not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Вариант, который вызывающий не видит в списке, не должен читаться и по id, —
	// иначе проверка чисто косметическая.
	if variant != nil && len(h.visibleTo(r, []*repository.ServiceNode{variant})) == 0 {
		http.Error(w, "variant not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{
		"variant": variant,
		"path":    path,
	})
}

// AdminListBehaviors обслуживает GET /admin/service-behaviors. Он возвращает
// библиотечные поведения, поставляемые со сборкой, каждое с полным текстом:
// конструктор услуг показывает их как стартовый шаблон особой услуги, и именно
// чтением такого скрипта админ узнаёт, что скрипту вообще позволено.
//
// Скрипты отдельных узлов здесь намеренно не перечисляются — скрипт узла есть
// часть этого узла и правится на нём.
func (h *ServiceCatalogHandler) AdminListBehaviors(w http.ResponseWriter, r *http.Request) {
	if h.behaviors == nil || h.behaviors.Engine() == nil {
		writeJSON(w, []interface{}{})
		return
	}
	writeJSON(w, h.behaviors.Engine().Library())
}

// AdminListNodes обслуживает GET /admin/service-nodes. Списанные узлы не
// показываются, пока вызывающий не попросит их через include_deleted=true.
func (h *ServiceCatalogHandler) AdminListNodes(w http.ResponseWriter, r *http.Request) {
	filter := repository.ServiceNodeFilter{IncludeDeleted: queryBool(r, "include_deleted")}

	roots, err := h.catalogRepo.GetRootCategories(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make([]map[string]interface{}, 0, len(roots))
	for _, root := range roots {
		result = append(result, h.buildTree(r.Context(), root, filter))
	}
	writeJSON(w, result)
}

func (h *ServiceCatalogHandler) buildTree(ctx context.Context, node *repository.ServiceNode, filter repository.ServiceNodeFilter) map[string]interface{} {
	children, _ := h.catalogRepo.GetChildren(ctx, node.ID, filter)
	childTrees := make([]map[string]interface{}, 0, len(children))
	for _, child := range children {
		childTrees = append(childTrees, h.buildTree(ctx, child, filter))
	}
	return map[string]interface{}{
		"node":     node,
		"children": childTrees,
	}
}

// AdminGetNode обслуживает GET /admin/service-nodes/:id.
func (h *ServiceCatalogHandler) AdminGetNode(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}
	node, err := h.catalogRepo.GetNodeByID(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, node)
}

// AdminCreateNode обслуживает POST /admin/service-nodes.
func (h *ServiceCatalogHandler) AdminCreateNode(w http.ResponseWriter, r *http.Request) {
	var req repository.ServiceNode
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.validateNode(&req, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.validateParent(r.Context(), req.ParentID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.catalogRepo.CreateNode(r.Context(), &req); err != nil {
		writeCatalogError(w, err)
		return
	}
	// Скрипт компилируется в работающий движок сразу, поэтому услуга ведёт себя
	// как отредактировано уже на следующем запросе, а не после перезапуска.
	h.syncBehavior(&req)

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, req)
}

// AdminUpdateNode обслуживает PUT /admin/service-nodes/:id.
func (h *ServiceCatalogHandler) AdminUpdateNode(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	var req repository.ServiceNode
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.ID = id

	// code и node_type неизменяемы, поэтому клиенты не шлют их при обновлении.
	// Проверка запроса по его же пустому node_type раньше отклоняла любую правку
	// варианта с «CATEGORY cannot have base_price»; правила применяются к
	// сохранённому узлу.
	existing, err := h.catalogRepo.GetNodeByID(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if existing.IsDeleted() {
		http.Error(w, "node is deleted: restore it before editing", http.StatusConflict)
		return
	}
	req.NodeType = existing.NodeType
	req.Code = existing.Code

	if err := h.validateNode(&req, false); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.validateParent(r.Context(), req.ParentID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.catalogRepo.UpdateNode(r.Context(), &req); err != nil {
		writeCatalogError(w, err)
		return
	}
	h.syncBehavior(&req)

	writeJSON(w, req)
}

// AdminDeleteNode обслуживает DELETE /admin/service-nodes/:id. Узел списывается,
// а не удаляется: у размещённых по нему заказов остаётся их услуга, а сам узел
// можно позже восстановить.
func (h *ServiceCatalogHandler) AdminDeleteNode(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	// Читаем перед удалением: ответ после него не изменился бы, но админ-панель
	// хочет сказать, что история заказов сохраняется.
	hadOrders, _ := h.catalogRepo.HasOrders(r.Context(), id)

	// Удаление каскадное, поэтому и снятие поведений — по всему поддереву.
	// Собираем живых потомков до удаления: после него GetDescendants их уже не
	// вернёт (они отфильтруются по deleted_at). depth-0 (сам узел) добавляем
	// отдельно, потому что GetDescendants отдаёт только потомков.
	subtree := []uuid.UUID{id}
	if descendants, err := h.catalogRepo.GetDescendants(r.Context(), id, nil); err == nil {
		for _, d := range descendants {
			subtree = append(subtree, d.ID)
		}
	}

	if err := h.catalogRepo.DeleteNode(r.Context(), id); err != nil {
		writeCatalogError(w, err)
		return
	}
	// Списанный узел немедленно перестаёт выполнять свой скрипт; строка его
	// хранит, поэтому восстановление возвращает услугу ровно такой, какой она была.
	if h.behaviors != nil {
		for _, nodeID := range subtree {
			h.behaviors.RemoveNode(nodeID)
		}
	}

	writeJSON(w, map[string]interface{}{
		"message":    "node deleted successfully",
		"soft":       true,
		"had_orders": hadOrders,
		// Сколько узлов ушло вместе с этим (узел + поддерево), чтобы админ-панель
		// могла сказать «удалена категория и N вложенных элементов».
		"deleted_count": len(subtree),
	})
}

// AdminRestoreNode обслуживает POST /admin/service-nodes/:id/restore. Узел
// возвращается выключенным, чтобы его публиковали заново осознанно.
func (h *ServiceCatalogHandler) AdminRestoreNode(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	if err := h.catalogRepo.RestoreNode(r.Context(), id); err != nil {
		writeCatalogError(w, err)
		return
	}

	node, err := h.catalogRepo.GetNodeByID(r.Context(), id)
	if err != nil {
		writeJSON(w, map[string]string{"message": "node restored successfully"})
		return
	}
	h.syncBehavior(node)
	writeJSON(w, node)
}

// syncBehavior регистрирует (или снимает с регистрации) собственный скрипт узла
// в работающем движке. Скрипт уже скомпилирован в validateNode, поэтому сбой
// здесь — неожиданность, которую стоит залогировать; страхует периодическая
// пересинхронизация в воркере поведений, она же разносит правку по процессам.
func (h *ServiceCatalogHandler) syncBehavior(node *repository.ServiceNode) {
	if h.behaviors == nil {
		return
	}
	if err := h.behaviors.SyncNode(node); err != nil {
		log.Printf("[behavior] node %s saved but not loaded: %v", node.Code, err)
	}
}

// writeCatalogError сопоставляет ошибки репозитория с кодами статуса, чтобы
// админ-панель отличала конфликт от бага.
func writeCatalogError(w http.ResponseWriter, err error) {
	switch {
	case isNotFound(err):
		http.Error(w, "node not found", http.StatusNotFound)
	case errors.Is(err, repository.ErrServiceNodeParentCycle):
		// Узел просят увести под самого себя — это ошибка в запросе, а не сбой.
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, repository.ErrServiceNodeDeleted),
		errors.Is(err, repository.ErrServiceNodeNotDeleted),
		errors.Is(err, repository.ErrServiceNodeHasChildren),
		errors.Is(err, repository.ErrServiceNodeCodeTaken),
		errors.Is(err, repository.ErrServiceNodeParentDeleted):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// isNotFound покрывает вызовы репозитория, которые до сих пор отдают
// отсутствующую строку как sql.ErrNoRows.
func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrServiceNodeNotFound)
}

var codeRegexp = regexp.MustCompile(`^[a-z0-9_]+$`)

// maxScriptBytes ограничивает одно поле скрипта. Поведение — это страница
// правил, а не программа; предел стоит, чтобы случайная вставка не забила колонку.
const maxScriptBytes = 64 * 1024

func (h *ServiceCatalogHandler) validateParent(ctx context.Context, parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	parent, err := h.catalogRepo.GetNodeByID(ctx, *parentID)
	if err != nil {
		return errors.New("parent not found")
	}
	if parent == nil {
		return errors.New("parent not found")
	}
	if parent.NodeType != repository.ServiceNodeTypeCategory {
		return errors.New("parent must be a category")
	}
	// Узел под удалённой категорией был бы недостижим из каталога.
	if parent.IsDeleted() {
		return errors.New("parent category is deleted")
	}
	return nil
}

func (h *ServiceCatalogHandler) validateNode(node *repository.ServiceNode, isCreate bool) error {
	if isCreate {
		if node.Code == "" {
			return errors.New("code is required")
		}
		if !codeRegexp.MatchString(node.Code) {
			return errors.New("code must match ^[a-z0-9_]+$")
		}
		if node.NodeType != repository.ServiceNodeTypeCategory && node.NodeType != repository.ServiceNodeTypeVariant {
			return errors.New("node_type must be CATEGORY or VARIANT")
		}
	}

	if node.Name == nil || node.Name["ru"] == "" {
		return errors.New("name must contain at least the 'ru' key")
	}

	// Собственный скрипт узла компилируется здесь, до записи строки: скрипт,
	// который не компилируется, провалил бы все проверки узла, а заказчик прочёл бы
	// это как исчезновение услуги. Лучше отказать в сохранении, пока админ ещё
	// смотрит в редактор.
	if node.HasOwnScript() {
		if len(node.BehaviorSource) > maxScriptBytes || len(node.BehaviorConstants) > maxScriptBytes {
			return fmt.Errorf("скрипт длиннее %d КБ", maxScriptBytes/1024)
		}
		if h.behaviors == nil {
			return errors.New("service behaviors are not available on this server")
		}
		if err := h.behaviors.Validate(node); err != nil {
			return fmt.Errorf("скрипт не компилируется: %w", err)
		}
	} else if node.BehaviorCode != "" {
		// Библиотечный код, за которым нет скрипта, точно так же отказывает в безопасную сторону.
		if h.behaviors == nil || !h.behaviors.Engine().Has(node.BehaviorCode) {
			return errors.New("unknown behavior_code: " + node.BehaviorCode)
		}
	}

	if node.NodeType == repository.ServiceNodeTypeVariant {
		if node.BasePrice == nil {
			return errors.New("VARIANT must have base_price")
		}
		if node.IsAuction && *node.BasePrice != 0 {
			return errors.New("auction variant base_price must be 0")
		}
	} else {
		// Клиент, у которого одна форма на оба типа узлов, шлёт base_price: 0 для
		// категории. Это «нет цены», а не противоречащая цена.
		if node.BasePrice != nil && node.BasePrice.IsZero() {
			node.BasePrice = nil
		}
		if node.BasePrice != nil {
			return errors.New("CATEGORY cannot have base_price")
		}
	}

	return nil
}

// queryBool читает булев параметр запроса в тех написаниях, какие обычно
// встречаются в строке запроса браузера.
func queryBool(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	idStr := chi.URLParam(r, name)
	return uuid.Parse(idStr)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
