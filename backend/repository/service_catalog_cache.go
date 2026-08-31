package repository

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// cachedServiceCatalogRepo caches single-node lookups by id.
//
// Why only those: GetNodeByID and GetNodesByIDs are what the request path calls
// repeatedly — every order rendered in a list resolves its service variant, and
// the eligibility predicate reads the variant's flags for every order it judges.
// The tree-navigation methods are called once per screen and are left to the
// database, so the cache stays a small map of rows rather than a second copy of
// the catalog with its own invalidation rules.
//
// Freshness: every catalog mutation that goes through this repository flushes
// the cache, so an admin's edit is visible to the next request. The TTL is the
// backstop for the writes this process cannot see — a second replica, or psql —
// and bounds how long such a change can go unnoticed.
//
// Entries are shared pointers handed to callers, and the nested LocalizedText
// maps are shared with them. Nothing in the codebase writes to a node it read;
// treat what comes out of here as read-only, as the code already does.
type cachedServiceCatalogRepo struct {
	// Embedded so every method this cache does not care about passes straight
	// through to the real repository — including new ones added later, which
	// then simply go uncached rather than silently returning stale rows.
	ServiceCatalogRepository

	ttl time.Duration

	mu      sync.RWMutex
	entries map[uuid.UUID]cachedNode
}

type cachedNode struct {
	node    *ServiceNode
	expires time.Time
}

// NewCachedServiceCatalogRepository wraps a catalog repository with an in-memory
// cache of node-by-id lookups. A ttl of zero disables caching entirely and
// returns the inner repository unchanged.
func NewCachedServiceCatalogRepository(inner ServiceCatalogRepository, ttl time.Duration) ServiceCatalogRepository {
	if ttl <= 0 {
		return inner
	}
	return &cachedServiceCatalogRepo{
		ServiceCatalogRepository: inner,
		ttl:                      ttl,
		entries:                  make(map[uuid.UUID]cachedNode),
	}
}

func (r *cachedServiceCatalogRepo) lookup(id uuid.UUID) (*ServiceNode, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[id]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	return entry.node, true
}

func (r *cachedServiceCatalogRepo) store(id uuid.UUID, node *ServiceNode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[id] = cachedNode{node: node, expires: time.Now().Add(r.ttl)}
}

// flush drops everything. Catalog mutations are rare admin actions, so a whole
// flush is cheaper to reason about than working out which nodes an edit could
// have affected — moving a node changes what its descendants resolve to.
func (r *cachedServiceCatalogRepo) flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = make(map[uuid.UUID]cachedNode)
}

func (r *cachedServiceCatalogRepo) GetNodeByID(ctx context.Context, id uuid.UUID) (*ServiceNode, error) {
	if node, ok := r.lookup(id); ok {
		return node, nil
	}
	node, err := r.ServiceCatalogRepository.GetNodeByID(ctx, id)
	if err != nil {
		// Misses are not cached: sql.ErrNoRows for an id that is about to exist
		// (a node created by another process) would otherwise stick for the TTL.
		return nil, err
	}
	r.store(id, node)
	return node, nil
}

func (r *cachedServiceCatalogRepo) GetNodesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*ServiceNode, error) {
	result := make(map[uuid.UUID]*ServiceNode, len(ids))
	missing := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if node, ok := r.lookup(id); ok {
			if node != nil {
				result[id] = node
			}
			continue
		}
		missing = append(missing, id)
	}
	if len(missing) == 0 {
		return result, nil
	}

	loaded, err := r.ServiceCatalogRepository.GetNodesByIDs(ctx, missing)
	if err != nil {
		return nil, err
	}
	for id, node := range loaded {
		r.store(id, node)
		result[id] = node
	}
	return result, nil
}

func (r *cachedServiceCatalogRepo) CreateNode(ctx context.Context, node *ServiceNode) error {
	err := r.ServiceCatalogRepository.CreateNode(ctx, node)
	r.flush()
	return err
}

func (r *cachedServiceCatalogRepo) UpdateNode(ctx context.Context, node *ServiceNode) error {
	err := r.ServiceCatalogRepository.UpdateNode(ctx, node)
	r.flush()
	return err
}

func (r *cachedServiceCatalogRepo) DeleteNode(ctx context.Context, id uuid.UUID) error {
	err := r.ServiceCatalogRepository.DeleteNode(ctx, id)
	r.flush()
	return err
}

func (r *cachedServiceCatalogRepo) RestoreNode(ctx context.Context, id uuid.UUID) error {
	err := r.ServiceCatalogRepository.RestoreNode(ctx, id)
	r.flush()
	return err
}
