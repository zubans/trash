package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"healthlogin/backend/middleware"
	"healthlogin/backend/service"
)

// ServiceCatalogHandler обслуживает публичные и админские HTTP-эндпоинты
// каталога услуг. Видимость каталога решает service.ServiceCatalog, правила
// конструктора — service.ServiceCatalogAdmin.
type ServiceCatalogHandler struct {
	catalog *service.ServiceCatalog
	admin   *service.ServiceCatalogAdmin
}

// NewServiceCatalogHandler создаёт ServiceCatalogHandler.
func NewServiceCatalogHandler(catalog *service.ServiceCatalog, admin *service.ServiceCatalogAdmin) *ServiceCatalogHandler {
	return &ServiceCatalogHandler{catalog: catalog, admin: admin}
}

// RegisterPublicRoutes — каталог для заказчика. Вызывающий подключает их под
// OptionalAuth, чтобы каталог мог прятать услуги «только для верифицированных»
// от неверифицированных заказчиков, оставаясь доступным анонимным посетителям.
func (h *ServiceCatalogHandler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/service-categories", h.ListRootCategories)
	r.Get("/service-categories/{id}/children", h.ListChildren)
	r.Get("/service-categories/{id}/variants", h.ListCategoryVariants)
	r.Get("/service-variants", h.ListVariants)
	r.Get("/service-variants/{id}", h.GetVariant)
}

// RegisterAdminRoutes — конструктор услуг.
func (h *ServiceCatalogHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("service_catalog.view")).Get("/admin/service-behaviors", h.AdminListBehaviors)
	r.With(can("service_catalog.view")).Get("/admin/service-nodes", h.AdminListNodes)
	r.With(can("service_catalog.view")).Get("/admin/service-nodes/{id}", h.AdminGetNode)
	r.With(can("service_catalog.create")).Post("/admin/service-nodes", h.AdminCreateNode)
	r.With(can("service_catalog.edit")).Put("/admin/service-nodes/{id}", h.AdminUpdateNode)
	r.With(can("service_catalog.delete")).Delete("/admin/service-nodes/{id}", h.AdminDeleteNode)
	r.With(can("service_catalog.edit")).Post("/admin/service-nodes/{id}/restore", h.AdminRestoreNode)
}

// writeCatalogError отвечает по классу ошибки. Ошибка ввода конструктора —
// 400, как и раньше: формы админ-панели показывают её текст как есть; всё
// остальное — по общему правилу writeDomainError.
func writeCatalogError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrValidation) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeDomainError(w, err)
}

// ListRootCategories обслуживает GET /service-categories.
func (h *ServiceCatalogHandler) ListRootCategories(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.catalog.RootCategories(r.Context(), middleware.UserFrom(r))
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

// ListChildren обслуживает GET /service-categories/{id}/children.
func (h *ServiceCatalogHandler) ListChildren(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id", "category id")
	if !ok {
		return
	}
	nodes, err := h.catalog.Children(r.Context(), middleware.UserFrom(r), id)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

// ListCategoryVariants обслуживает GET /service-categories/{id}/variants.
func (h *ServiceCatalogHandler) ListCategoryVariants(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id", "category id")
	if !ok {
		return
	}
	nodes, err := h.catalog.CategoryVariants(r.Context(), middleware.UserFrom(r), id)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

// ListVariants обслуживает GET /service-variants.
func (h *ServiceCatalogHandler) ListVariants(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.catalog.Variants(r.Context(), middleware.UserFrom(r))
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

// GetVariant обслуживает GET /service-variants/{id}.
func (h *ServiceCatalogHandler) GetVariant(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id", "variant id")
	if !ok {
		return
	}
	out, err := h.catalog.Variant(r.Context(), middleware.UserFrom(r), id)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminListBehaviors обслуживает GET /admin/service-behaviors — библиотечные
// поведения со сборки, каждое с полным текстом.
func (h *ServiceCatalogHandler) AdminListBehaviors(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.admin.Library())
}

// AdminListNodes обслуживает GET /admin/service-nodes?include_deleted=true.
func (h *ServiceCatalogHandler) AdminListNodes(w http.ResponseWriter, r *http.Request) {
	tree, err := h.admin.Tree(r.Context(), queryBool(r, "include_deleted"))
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

// AdminGetNode обслуживает GET /admin/service-nodes/{id}.
func (h *ServiceCatalogHandler) AdminGetNode(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id", "node id")
	if !ok {
		return
	}
	node, err := h.admin.Get(r.Context(), id)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

// AdminCreateNode обслуживает POST /admin/service-nodes.
func (h *ServiceCatalogHandler) AdminCreateNode(w http.ResponseWriter, r *http.Request) {
	var form service.ServiceNodeForm
	if !decodeBody(w, r, &form) {
		return
	}
	node, err := h.admin.Create(r.Context(), form)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, node)
}

// AdminUpdateNode обслуживает PUT /admin/service-nodes/{id}.
func (h *ServiceCatalogHandler) AdminUpdateNode(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id", "node id")
	if !ok {
		return
	}
	var form service.ServiceNodeForm
	if !decodeBody(w, r, &form) {
		return
	}
	node, err := h.admin.Update(r.Context(), id, form)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

// AdminDeleteNode обслуживает DELETE /admin/service-nodes/{id}.
func (h *ServiceCatalogHandler) AdminDeleteNode(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id", "node id")
	if !ok {
		return
	}
	result, err := h.admin.Delete(r.Context(), id)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// AdminRestoreNode обслуживает POST /admin/service-nodes/{id}/restore.
func (h *ServiceCatalogHandler) AdminRestoreNode(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id", "node id")
	if !ok {
		return
	}
	node, err := h.admin.Restore(r.Context(), id)
	if err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}
