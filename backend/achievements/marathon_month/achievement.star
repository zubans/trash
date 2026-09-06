# Марафонец: 50 подтверждённых заказов за календарный месяц.
#
# Повторяемая — но не чаще раза в месяц, и это выражено ключом выдачи, а не
# проверкой: ключ равен месяцу, а уникальность (пользователь, ачивка, ключ)
# сделает вторую выдачу за тот же месяц невозможной, даже если событие придёт
# пятьдесят первым заказом.
#
# Требование разных заказчиков — не украшение. Ачивка с подарком, зависящая
# только от числа заказов, зарабатывается сговором с одним человеком; чтобы
# сговор перестал окупаться, заказчиков должно быть много.

MANIFEST = {
    "title": "Марафонец",
    "description": "50 выполненных заказов за календарный месяц.",
    "icon": "medal",
    "audience": "EXECUTOR",
    "events": [EVENT_ORDER_CONFIRMED],
    "once_per_user": False,
    "weight": WEIGHT,
    "defaults": {
        CFG_TARGET_ORDERS: TARGET_ORDERS,
        CFG_WEIGHT: WEIGHT,
        CFG_GIFT_CODE: GIFT_CODE,
        CFG_MIN_DISTINCT_CUSTOMERS: MIN_DISTINCT_CUSTOMERS,
    },
}

def _month_key(f):
    # Ключ выдачи — календарный месяц, который ядро передаёт готовой строкой
    # "2026-09". Ачивку открывает пятидесятый заказ, и относится она к тому
    # месяцу, когда он был подтверждён.
    return "month:" + f.month

def check(f):
    o = f.order
    if o == None or f.user == None:
        return None
    if o.executor_id != f.user.id:
        return None
    if f.stats.orders_completed_month < f.config[CFG_TARGET_ORDERS]:
        return None

    effects = [notify(subject = MSG_SUBJECT, text = MSG_TEXT)]
    code = f.config[CFG_GIFT_CODE]
    if code != None and f.stats.distinct_customers >= f.config[CFG_MIN_DISTINCT_CUSTOMERS]:
        effects.append(gift(code = code))

    return grant(
        key = _month_key(f),
        points = f.config[CFG_WEIGHT],
        order_id = o.id,
        reason = "50 заказов за месяц",
        effects = effects,
    )

def progress(f):
    target = f.config[CFG_TARGET_ORDERS]
    if target <= 0:
        return None
    return float(f.stats.orders_completed_month) / float(target)
