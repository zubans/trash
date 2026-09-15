# Первое покаяние.
#
# Исполнитель, признавший оспоренный заказ до решения арбитра, получает значок и
# баллы — один раз за всё время. Признание отменяет заказ без штрафного балла;
# ачивка поощряет честность, а не заработок, поэтому ядро не требует от заказа
# оплаты (eligibleForConcession).
#
# Признанный заказ уже отменён, поэтому ядро не привязывает выдачу к нему и
# отмена заказа её не отзывает: отзывать эту выдачу не за что.

MANIFEST = {
    "title": "Первое покаяние",
    "description": "Признать оспоренный заказ невыполненным до решения арбитра.",
    "icon": "handshake",
    "audience": "EXECUTOR",
    "events": [EVENT_DISPUTE_CONCEDED],
    "once_per_user": True,
    "weight": WEIGHT,
    "defaults": {
        CFG_WEIGHT: WEIGHT,
    },
}

def check(f):
    o = f.order
    if o == None or f.user == None:
        return None
    # Признаёт исполнитель; событие о заказе приходит и заказчику, и его
    # аудитория здесь не та, но проверка стоит того, чтобы быть явной.
    if o.executor_id != f.user.id:
        return None
    return grant(
        points = f.config[CFG_WEIGHT],
        reason = "признание оспоренного заказа",
        effects = [notify(subject = MSG_SUBJECT, text = MSG_TEXT)],
    )

def progress(f):
    if "first_repentance" in f.granted:
        return 1.0
    return 0.0
