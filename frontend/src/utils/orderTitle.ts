import type { ServiceNode } from '../api/services'

/**
 * How an order is named on screen: the category carries the title, the exact
 * service sits under it in a smaller font. Both dashboards used to build this
 * themselves — the customer's rendered "Категория (Услуга)" while the executor's
 * showed only the service and left the bold category line empty.
 */
export interface OrderTitle {
  /** Always filled: the category, or the service itself when it has no parent. */
  title: string
  /** The exact service. Empty when it would only repeat the title. */
  subtitle: string
}

export interface ServiceLookup {
  /** Service variants by id, for orders that carry only service_variant_id. */
  variants?: Record<string, ServiceNode>
  /** Categories, used to resolve a variant's parent. */
  categories?: ServiceNode[]
}

export function localizedNodeName(node?: ServiceNode | null): string {
  if (!node || !node.name) return ''
  return node.name['ru'] || node.name['en'] || node.code || ''
}

/**
 * orderTitle resolves an order's category and service. The order may carry the
 * whole variant object or just its id, so both are accepted; anything that
 * cannot be resolved degrades to a plain "Заказ" rather than an empty line.
 */
export function orderTitle(order: any, lookup: ServiceLookup = {}): OrderTitle {
  const variant: ServiceNode | undefined =
    order?.service_variant ||
    (order?.service_variant_id ? lookup.variants?.[order.service_variant_id] : undefined)

  const variantName = localizedNodeName(variant)
  if (!variantName) {
    return { title: 'Заказ', subtitle: '' }
  }

  // Категория приезжает вместе с заказом. Локальный справочник — запасной путь
  // для заказов, отданных без неё: он держит только корневые категории
  // (/service-categories отдаёт лишь их), поэтому у вложенного каталога
  // родителя варианта в нём не найти.
  const parent =
    order?.service_category ||
    (variant?.parent_id ? lookup.categories?.find((c) => c.id === variant.parent_id) : undefined)
  const categoryName = localizedNodeName(parent)

  if (!categoryName || categoryName === variantName) {
    return { title: variantName, subtitle: '' }
  }
  return { title: categoryName, subtitle: variantName }
}

/** The same name on one line, for tables and other single-line places. */
export function orderTitleLine(order: any, lookup: ServiceLookup = {}): string {
  const { title, subtitle } = orderTitle(order, lookup)
  return subtitle ? `${title} · ${subtitle}` : title
}
