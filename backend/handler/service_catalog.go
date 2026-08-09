package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// ServiceCatalogHandler handles public and admin service catalog HTTP endpoints.
type ServiceCatalogHandler struct {
	catalogRepo repository.ServiceCatalogRepository
}

// NewServiceCatalogHandler creates a ServiceCatalogHandler.
func NewServiceCatalogHandler(catalogRepo repository.ServiceCatalogRepository) *ServiceCatalogHandler {
	return &ServiceCatalogHandler{catalogRepo: catalogRepo}
}

// ListRootCategories handles GET /service-categories.
func (h *ServiceCatalogHandler) ListRootCategories(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.catalogRepo.GetRootCategories(true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, nodes)
}

// ListChildren handles GET /service-categories/:id/children.
func (h *ServiceCatalogHandler) ListChildren(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid category id", http.StatusBadRequest)
		return
	}
	nodes, err := h.catalogRepo.GetChildren(id, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, nodes)
}

// ListCategoryVariants handles GET /service-categories/:id/variants.
func (h *ServiceCatalogHandler) ListCategoryVariants(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid category id", http.StatusBadRequest)
		return
	}
	nodes, err := h.catalogRepo.GetDescendants(id, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Return only active variants.
	variants := make([]*repository.ServiceNode, 0, len(nodes))
	for _, n := range nodes {
		if n.IsVariant() && n.IsActive {
			variants = append(variants, n)
		}
	}
	writeJSON(w, variants)
}

// ListVariants handles GET /service-variants.
func (h *ServiceCatalogHandler) ListVariants(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.catalogRepo.GetActiveVariants()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, nodes)
}

// GetVariant handles GET /service-variants/:id.
func (h *ServiceCatalogHandler) GetVariant(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid variant id", http.StatusBadRequest)
		return
	}
	variant, path, err := h.catalogRepo.GetVariantWithCategory(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "variant not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"variant": variant,
		"path":    path,
	})
}

// AdminListNodes handles GET /admin/service-nodes.
func (h *ServiceCatalogHandler) AdminListNodes(w http.ResponseWriter, r *http.Request) {
	roots, err := h.catalogRepo.GetRootCategories(false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make([]map[string]interface{}, 0, len(roots))
	for _, root := range roots {
		result = append(result, h.buildTree(root))
	}
	writeJSON(w, result)
}

func (h *ServiceCatalogHandler) buildTree(node *repository.ServiceNode) map[string]interface{} {
	children, _ := h.catalogRepo.GetChildren(node.ID, false)
	childTrees := make([]map[string]interface{}, 0, len(children))
	for _, child := range children {
		childTrees = append(childTrees, h.buildTree(child))
	}
	return map[string]interface{}{
		"node":     node,
		"children": childTrees,
	}
}

// AdminGetNode handles GET /admin/service-nodes/:id.
func (h *ServiceCatalogHandler) AdminGetNode(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}
	node, err := h.catalogRepo.GetNodeByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, node)
}

// AdminCreateNode handles POST /admin/service-nodes.
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

	if err := h.validateParent(req.ParentID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.catalogRepo.CreateNode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, req)
}

// AdminUpdateNode handles PUT /admin/service-nodes/:id.
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

	if err := h.validateNode(&req, false); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.validateParent(req.ParentID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.catalogRepo.UpdateNode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, req)
}

// AdminDeleteNode handles DELETE /admin/service-nodes/:id.
func (h *ServiceCatalogHandler) AdminDeleteNode(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	if err := h.catalogRepo.DeleteNode(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, map[string]string{"message": "node deleted successfully"})
}

var codeRegexp = regexp.MustCompile(`^[a-z0-9_]+$`)

func (h *ServiceCatalogHandler) validateParent(parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	parent, err := h.catalogRepo.GetNodeByID(*parentID)
	if err != nil {
		return errors.New("parent not found")
	}
	if parent == nil {
		return errors.New("parent not found")
	}
	if parent.NodeType != repository.ServiceNodeTypeCategory {
		return errors.New("parent must be a category")
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

	if node.NodeType == repository.ServiceNodeTypeVariant {
		if node.BasePrice == nil {
			return errors.New("VARIANT must have base_price")
		}
		if node.IsAuction && *node.BasePrice != 0 {
			return errors.New("auction variant base_price must be 0")
		}
	} else {
		if node.BasePrice != nil {
			return errors.New("CATEGORY cannot have base_price")
		}
	}

	return nil
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	idStr := chi.URLParam(r, name)
	return uuid.Parse(idStr)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
