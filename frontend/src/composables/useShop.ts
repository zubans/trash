import { useCachedResource } from './useCachedResource'
import { dropCache } from '../services/cache'
import { getMyPerks, getStorefront, type MyPerks, type Storefront } from '../api/shop'

// Данные магазина, которые нужны не только самому магазину: открыт ли он (от
// этого зависит пункт меню) и действующая привилегия (плашка на дашборде
// исполнителя). Оба — по правилам doc/frontend_data_loading.md: сначала кэш,
// потом сеть, без прелоадера поверх уже известного.

export const SHOP_STOREFRONT_KEY = 'shop:storefront'
export const SHOP_PERKS_KEY = 'shop:perks'
export const SHOP_ORDERS_KEY = 'shop:orders'

export function useStorefront() {
  return useCachedResource<Storefront>({
    key: SHOP_STOREFRONT_KEY,
    initial: { enabled: false, offer_version: 1, products: [] },
    fetcher: () => getStorefront(),
  })
}

export function useMyPerks() {
  return useCachedResource<MyPerks | null>({
    key: SHOP_PERKS_KEY,
    initial: null,
    fetcher: () => getMyPerks(),
  })
}

// После покупки кэш плашки и списка покупок устаревает сразу: плашка обязана
// появиться без перезагрузки, а покупка — в «Моих покупках».
export function forgetShopPurchaseCaches() {
  dropCache(SHOP_PERKS_KEY)
  dropCache(SHOP_ORDERS_KEY)
  dropCache(SHOP_STOREFRONT_KEY)
}
