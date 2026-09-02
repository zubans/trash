package repository

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// cachedServiceCatalogRepo кэширует чтение отдельных узлов по id.
//
// Почему только их: GetNodeByID и GetNodesByIDs — то, что путь запроса вызывает
// снова и снова: каждый заказ в списке разрешает свой вариант услуги, а предикат
// допуска читает флаги варианта для каждого оцениваемого заказа. Методы
// навигации по дереву вызываются раз на экран и оставлены базе, поэтому кэш
// остаётся маленькой картой строк, а не второй копией каталога со своими
// правилами инвалидации.
//
// Свежесть: любая мутация каталога, идущая через этот репозиторий, сбрасывает
// кэш, поэтому правка админа видна следующему запросу. TTL — страховка для
// записей, которых этот процесс не видит (вторая реплика или psql), и он
// ограничивает, как долго такое изменение может остаться незамеченным.
//
// Записи — общие указатели, отдаваемые вызывающим, и вложенные карты
// LocalizedText разделяются с ними. Ничто в кодовой базе не пишет в прочитанный
// узел; считайте выходящее отсюда доступным только для чтения, как код и делает.
type cachedServiceCatalogRepo struct {
	// Встроен так, что любой метод, до которого этому кэшу нет дела, проходит
	// прямо в настоящий репозиторий — включая новые, добавленные позже, которые
	// тогда просто не кэшируются, а не возвращают молча устаревшие строки.
	ServiceCatalogRepository

	ttl time.Duration

	mu      sync.RWMutex
	entries map[uuid.UUID]cachedNode
}

type cachedNode struct {
	node    *ServiceNode
	expires time.Time
}

// NewCachedServiceCatalogRepository оборачивает репозиторий каталога кэшем
// чтений узла по id в памяти. Нулевой ttl полностью выключает кэширование и
// возвращает внутренний репозиторий без изменений.
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

// flush сбрасывает всё. Мутации каталога — редкие действия админа, поэтому
// полный сброс проще для понимания, чем вычисление того, какие узлы могла
// затронуть правка: перенос узла меняет то, во что разрешаются его потомки.
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
		// Промахи не кэшируются: sql.ErrNoRows для id, который вот-вот появится
		// (узел, созданный другим процессом), иначе залип бы на весь TTL.
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
