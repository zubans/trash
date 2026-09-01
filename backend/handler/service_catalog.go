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

// ServiceCatalogHandler handles public and admin service catalog HTTP endpoints.
type ServiceCatalogHandler struct {
	catalogRepo repository.ServiceCatalogRepository
	// behaviors decides visibility for nodes whose rules come from a script.
	// Optional: without it only the built-in flags apply, which is what the
	// catalog did before behaviours existed.
	behaviors *service.Behaviors
}

// NewServiceCatalogHandler creates a ServiceCatalogHandler.
func NewServiceCatalogHandler(catalogRepo repository.ServiceCatalogRepository) *ServiceCatalogHandler {
	return &ServiceCatalogHandler{catalogRepo: catalogRepo}
}

// WithBehaviors wires the behaviour scripts into the catalog listings.
func (h *ServiceCatalogHandler) WithBehaviors(behaviors *service.Behaviors) *ServiceCatalogHandler {
	h.behaviors = behaviors
	return h
}

// hideVerificationOnly reports whether the requester is a customer who has not
// been manually verified. Such customers must not see services flagged
// requires_verification — they cannot order them (enforced at order creation),
// so listing them would only mislead. Executors, admins and anonymous visitors
// are left unaffected; this is populated by the OptionalAuth middleware.
func hideVerificationOnly(r *http.Request) bool {
	user := userFromContext(r)
	return user != nil && user.Role == "CUSTOMER" && !user.IsVerified()
}

// visibleTo drops the nodes this requester must not see: the ones flagged
// requires_verification when they are an unverified customer, and the ones a
// behaviour script hides from them.
//
// Both rules are applied here, in one pass, because a node the caller cannot
// order must not be listed either — a listing that shows what checkout will
// refuse is worse than not listing it at all. The claim counts are read once
// per request, not once per node.
func (h *ServiceCatalogHandler) visibleTo(r *http.Request, nodes []*repository.ServiceNode) []*repository.ServiceNode {
	hide := hideVerificationOnly(r)
	user := userFromContext(r)

	// Claims are read only when something on this page can use them. A catalog
	// of ordinary services must not gain a query per request because scripted
	// services exist elsewhere.
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

// ListRootCategories handles GET /service-categories.
func (h *ServiceCatalogHandler) ListRootCategories(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.catalogRepo.GetRootCategories(r.Context(), repository.FilterActive)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.visibleTo(r, nodes))
}

// ListChildren handles GET /service-categories/:id/children.
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

// ListCategoryVariants handles GET /service-categories/:id/variants.
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
	// Return only active variants.
	variants := make([]*repository.ServiceNode, 0, len(nodes))
	for _, n := range nodes {
		if n.IsVariant() && n.IsActive {
			variants = append(variants, n)
		}
	}
	writeJSON(w, h.visibleTo(r, variants))
}

// ListVariants handles GET /service-variants.
func (h *ServiceCatalogHandler) ListVariants(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.catalogRepo.GetActiveVariants(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.visibleTo(r, nodes))
}

// GetVariant handles GET /service-variants/:id.
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
	// A variant the caller cannot see in a listing must not be readable by id
	// either — otherwise the gate is only cosmetic.
	if variant != nil && len(h.visibleTo(r, []*repository.ServiceNode{variant})) == 0 {
		http.Error(w, "variant not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{
		"variant": variant,
		"path":    path,
	})
}

// AdminListBehaviors handles GET /admin/service-behaviors. It returns the
// library behaviours that ship with the build, each with its full text: the
// service constructor shows them as the starting template for a special
// service, and reading one is how an admin learns what a script may do.
//
// Per-node scripts are deliberately not listed here — a node's script is part of
// that node and is edited on it.
func (h *ServiceCatalogHandler) AdminListBehaviors(w http.ResponseWriter, r *http.Request) {
	if h.behaviors == nil || h.behaviors.Engine() == nil {
		writeJSON(w, []interface{}{})
		return
	}
	writeJSON(w, h.behaviors.Engine().Library())
}

// AdminListNodes handles GET /admin/service-nodes. Retired nodes are left out
// unless the caller asks for them with include_deleted=true.
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

// AdminGetNode handles GET /admin/service-nodes/:id.
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

	if err := h.validateParent(r.Context(), req.ParentID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.catalogRepo.CreateNode(r.Context(), &req); err != nil {
		writeCatalogError(w, err)
		return
	}
	// The script is compiled into the running engine now, so the service behaves
	// as edited on the very next request rather than after a restart.
	h.syncBehavior(&req)

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

	// code and node_type are immutable, so clients do not send them on update.
	// Validating the request against its own empty node_type used to reject
	// every variant edit with "CATEGORY cannot have base_price"; the rules
	// apply to the stored node.
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

// AdminDeleteNode handles DELETE /admin/service-nodes/:id. The node is retired,
// not removed: orders placed for it keep their service, and the node can be
// restored later.
func (h *ServiceCatalogHandler) AdminDeleteNode(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	// Read before deleting: the answer would not change afterwards, but the
	// admin panel wants to say that order history is being kept.
	hadOrders, _ := h.catalogRepo.HasOrders(r.Context(), id)

	if err := h.catalogRepo.DeleteNode(r.Context(), id); err != nil {
		writeCatalogError(w, err)
		return
	}
	// A retired node stops running its script at once; the row keeps it, so a
	// restore brings the service back exactly as it was.
	if h.behaviors != nil {
		h.behaviors.RemoveNode(id)
	}

	writeJSON(w, map[string]interface{}{
		"message":    "node deleted successfully",
		"soft":       true,
		"had_orders": hadOrders,
	})
}

// AdminRestoreNode handles POST /admin/service-nodes/:id/restore. The node comes
// back switched off so that it is re-published deliberately.
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

// syncBehavior registers (or unregisters) the node's own script in the running
// engine. The script has already been compiled by validateNode, so a failure
// here is a surprise worth logging; the periodic resync in the behaviour worker
// is what covers it, and what carries the edit to the other processes.
func (h *ServiceCatalogHandler) syncBehavior(node *repository.ServiceNode) {
	if h.behaviors == nil {
		return
	}
	if err := h.behaviors.SyncNode(node); err != nil {
		log.Printf("[behavior] node %s saved but not loaded: %v", node.Code, err)
	}
}

// writeCatalogError maps repository errors to status codes so the admin panel
// can tell a conflict from a bug.
func writeCatalogError(w http.ResponseWriter, err error) {
	switch {
	case isNotFound(err):
		http.Error(w, "node not found", http.StatusNotFound)
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

// isNotFound covers the repository calls that still surface a missing row as
// sql.ErrNoRows.
func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrServiceNodeNotFound)
}

var codeRegexp = regexp.MustCompile(`^[a-z0-9_]+$`)

// maxScriptBytes bounds one script field. A behaviour is a page of rules, not a
// program; the limit is here so a paste accident cannot fill a column.
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
	// A node under a deleted category would be unreachable from the catalog.
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

	// The node's own script is compiled here, before the row is written: a
	// script that does not compile would fail every gate on the node, which
	// reads to a customer as the service having disappeared. Better to refuse
	// the save while the admin is still looking at the editor.
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
		// A library code with no script behind it fails closed in the same way.
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
		// A client that keeps one form for both node types sends base_price: 0
		// for a category. That is "no price", not a conflicting price.
		if node.BasePrice != nil && node.BasePrice.IsZero() {
			node.BasePrice = nil
		}
		if node.BasePrice != nil {
			return errors.New("CATEGORY cannot have base_price")
		}
	}

	return nil
}

// queryBool reads a boolean query parameter in the spellings a browser query
// string tends to carry.
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
