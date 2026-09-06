# Самый быстрый стрелок на Диком Западе.
#
# Заказ, выполненный и подтверждённый в течение WINDOW_MINUTES после создания.
# Повторяемая: каждый подходящий заказ приносит свои баллы, поэтому ключ выдачи
# обязателен и равен id заказа — переотправленное событие находит выдачу на
# месте и не начисляет второй раз.
#
# Ачивка накручиваема сговором с заказчиком, и потому вся её защита — не здесь.
# Скрипт отсекает мелкие заказы, а остальное делает ядро: заказчик и исполнитель
# обязаны быть разными людьми, заказ — подтверждённым и оплаченным, а суточное
# начисление ограничено achievement_max_points_per_day. Отмена заказа задним
# числом отзывает выдачу вместе с баллами.

MANIFEST = {
    "title": "Самый быстрый стрелок на Диком Западе",
    "description": "Заказ выполнен в течение 20 минут после создания.",
    "icon": "revolver",
    "audience": "EXECUTOR",
    "events": [EVENT_ORDER_CONFIRMED],
    "once_per_user": False,
    "weight": WEIGHT,
    "lifetime_days": LIFETIME_DAYS,
    "defaults": {
        CFG_WINDOW_MINUTES: WINDOW_MINUTES,
        CFG_WEIGHT: WEIGHT,
        CFG_MIN_ORDER_AMOUNT: MIN_ORDER_AMOUNT,
        CFG_LIFETIME_DAYS: LIFETIME_DAYS,
    },
}

def _elapsed(o):
    # Метки времени приходят секундами; подтверждение — конец отсчёта, потому что
    # именно оно означает, что заказчик работу принял. Отметка «выполнено» самим
    # исполнителем концом отсчёта быть не может: её он ставит себе сам.
    if o.confirmed_at == None or o.created_at == None:
        return None
    return o.confirmed_at - o.created_at

def check(f):
    o = f.order
    if o == None or f.user == None:
        return None
    if o.executor_id != f.user.id:
        return None
    if o.amount < f.config[CFG_MIN_ORDER_AMOUNT]:
        return None

    elapsed = _elapsed(o)
    if elapsed == None or elapsed > minutes(f.config[CFG_WINDOW_MINUTES]):
        return None

    return grant(
        key = o.id,
        points = f.config[CFG_WEIGHT],
        lifetime_days = f.config[CFG_LIFETIME_DAYS],
        order_id = o.id,
        reason = "заказ выполнен за " + str(elapsed // 60) + " мин",
        effects = [notify(subject = MSG_SUBJECT, text = MSG_TEXT)],
    )

def progress(f):
    # Лучшее время исполнителя относительно окна: полоса показывает, насколько
    # он близок к попаданию, а не сколько раз уже попал.
    best = f.stats.fastest_completion_min
    if best == 0:
        return 0.0
    window = f.config[CFG_WINDOW_MINUTES]
    if best <= window:
        return 1.0
    # Вдвое медленнее окна — половина полосы.
    return float(window) / float(best)
