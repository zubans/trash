import api from '../services/api'

// Действия админки над заказом, общие для раздела «Заказы» и истории
// пользователя.

// Статусы заказа по-русски — одни и те же подписи во всех списках админки.
export const ORDER_STATUS_LABELS: Record<string, string> = {
  SEARCHING: 'в поиске',
  ASSIGNED: 'у исполнителя',
  EXECUTED: 'на проверке',
  DISPUTED: 'спор',
  COMPLETED: 'выполнен',
  CANCELED: 'отменён',
}

// Вернуть в работу можно только заказ на проверке: исполнитель отметил
// «Исполнил», заказчик ещё не подтвердил. Сервер проверяет то же самое.
export const canReturnToWork = (status: string) => status === 'EXECUTED'

export const RETURN_TO_WORK_CONFIRM =
  'Вернуть заказ в работу? Отметка «Исполнил» снимется, заказ снова окажется у исполнителя.'

export async function returnOrderToWork(orderID: string): Promise<void> {
  await api.post(`/admin/orders/${orderID}/return-to-work`)
}
