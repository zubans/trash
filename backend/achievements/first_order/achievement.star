# Первый заказ.
#
# Самая простая из возможных ачивок и потому — образец: одно условие, один
# эффект, никакой работы с историей. Выдаётся один раз за всё время, поэтому
# ключ выдачи скрипту называть не нужно: ядро подставит код ачивки, а уникальный
# индекс отклонит повтор, сколько бы раз событие ни доставили.

MANIFEST = {
    "title": "Первый заказ",
    "description": "Первый выполненный и подтверждённый заказ.",
    "icon": "trophy",
    "audience": "EXECUTOR",
    "events": [EVENT_ORDER_CONFIRMED],
    "once_per_user": True,
    "weight": WEIGHT,
    "defaults": {
        CFG_WEIGHT: WEIGHT,
        CFG_MIN_ORDER_AMOUNT: MIN_ORDER_AMOUNT,
        CFG_GIFT_CODE: GIFT_CODE,
    },
}

def check(f):
    o = f.order
    if o == None or f.user == None:
        return None
    # Ачивка исполнителя: заказ, где этот человек — заказчик, к ней отношения не
    # имеет. Что заказчик и исполнитель не совпадают, проверяет ядро.
    if o.executor_id != f.user.id:
        return None
    if o.amount < f.config[CFG_MIN_ORDER_AMOUNT]:
        return None
    # Нужен хотя бы один подтверждённый заказ — и этого достаточно. Сравнения
    # «ровно один» здесь быть не должно: счётчик к моменту разбора события уже
    # мог уйти дальше — ачивку включили, когда у человека были заказы, агрегаты
    # пересчитали админской командой, два подтверждения легли между тиками
    # диспетчера, — и тогда значок терялся навсегда. Второй раз его всё равно не
    # выдадут: ачивка разовая, ключ выдачи — её код, и повтор отклоняет
    # уникальный индекс, а не это условие.
    if f.stats.orders_completed < 1:
        return None

    effects = [notify(subject = MSG_SUBJECT, text = MSG_TEXT)]
    if f.config[CFG_GIFT_CODE] != None:
        effects.append(gift(code = f.config[CFG_GIFT_CODE]))

    return grant(
        points = f.config[CFG_WEIGHT],
        order_id = o.id,
        reason = "первый выполненный заказ",
        effects = effects,
    )

def progress(f):
    # Полоса на карточке «ещё не получено»: 0 или 1, промежуточного тут нет.
    if f.stats.orders_completed > 0:
        return 1.0
    return 0.0
