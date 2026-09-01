# Верификация аккаунта.
#
# Услуга, которую заказывает пользователь, чтобы подтвердить свою личность. Всё,
# что делает её особенной, живёт здесь, а не в коде Go:
#
#   * её видит только неподтверждённый пользователь и только пока не заказал её
#     один раз;
#   * она бесплатна;
#   * на стороне исполнителя её видит и может взять только VERIFIER_ROLE;
#   * модератору видно только адрес: ни телефона, ни ФИО, ни даты рождения. Он
#     вводит данные с документа, ядро сверяет их с учётной записью и сообщает
#     скрипту лишь результат сравнения;
#   * первое несовпадение — предупреждение сверить данные с паспортом, повторное
#     — заказ уходит на модерацию администратора;
#   * когда пользователь становится подтверждённым, заказ закрывается сам, а
#     вознаграждения из config.star выплачиваются.
#
# Суммы, роли, имена событий и тексты — в config.star; здесь только логика,
# которая их использует. Узел каталога может переопределить суммы и режим через
# service_nodes.behavior_config (ключи CFG_*).

MANIFEST = {
    "name": "Верификация аккаунта",
    "description": "Бесплатная услуга: модератор подтверждает личность пользователя и получает вознаграждение.",
    "once_per_user": True,
    "release_claim_on_cancel": True,
    "events": [EVENT_ORDER_EXECUTED, EVENT_USER_VERIFIED, EVENT_ORDER_SUBMISSION],
    # Какие поля модератор вводит с документа, и что он не видит о заказчике.
    "check_fields": CHECK_FIELDS,
    "hide_customer_contacts": HIDE_CUSTOMER_CONTACTS,
    "defaults": {
        CFG_REWARD_EXECUTOR: REWARD_EXECUTOR,
        CFG_REWARD_CUSTOMER: REWARD_CUSTOMER,
        CFG_APPLY_COMMISSION: APPLY_COMMISSION,
        CFG_VERIFIER_ROLE: VERIFIER_ROLE,
        CFG_VERIFIED_BY: VERIFIED_BY,
    },
}

# --- Каталог ------------------------------------------------------------------

def visible(f):
    # Подтверждённому пользователю здесь нечего заказывать, как и тому, у кого
    # заказ на верификацию уже есть: claims — это число сделанных заказов.
    if f.user == None:
        return False
    if f.user.is_verified:
        return False
    return f.claims == 0

def can_order(f):
    if f.user == None:
        return MSG_ANONYMOUS
    if f.user.is_verified:
        return MSG_ALREADY_VERIFIED
    if f.claims > 0:
        return MSG_ALREADY_ORDERED
    return None

def price(f):
    # Для заказчика бесплатно. Верификатору платит платформа, а не он.
    return 0

# --- Сторона исполнителя ------------------------------------------------------

def can_view_or_take(f):
    role = f.config.get(CFG_VERIFIER_ROLE, VERIFIER_ROLE)
    if f.viewer == None or not has_role(f.viewer, role):
        return MSG_MODERATORS_ONLY
    if f.viewer.status == "BANNED":
        return MSG_BANNED
    if f.customer != None and f.viewer.id == f.customer.id:
        return MSG_SELF_VERIFICATION
    return None

# --- Реакции на события -------------------------------------------------------

def _reward(f, o, recipient, amount, role):
    # Одна выплата с ключом, привязанным к заказу и к получателю: одно и то же
    # вознаграждение не будет выплачено дважды, сколько бы событий ни описывало
    # одну верификацию, а выплаты заказчику и исполнителю не перекроют друг друга.
    return pay_bonus(
        to = recipient,
        amount = amount,
        order_id = o.id,
        commission = f.config.get(CFG_APPLY_COMMISSION, APPLY_COMMISSION),
        key = REWARD_KEY_PREFIX + ":" + role + ":" + o.id,
        reason = "Вознаграждение за верификацию пользователя",
    )

def _reward_effects(f, o):
    effects = [
        complete_order(order_id = o.id, reason = "verified"),
        system_message(order_id = o.id, text = MSG_ORDER_CLOSED),
    ]
    to_executor = f.config.get(CFG_REWARD_EXECUTOR, REWARD_EXECUTOR)
    if o.executor_id != None and to_executor > 0:
        effects.append(_reward(f, o, o.executor_id, to_executor, "executor"))
    to_customer = f.config.get(CFG_REWARD_CUSTOMER, REWARD_CUSTOMER)
    if to_customer > 0:
        effects.append(_reward(f, o, o.customer_id, to_customer, "customer"))
    return effects

def _submission_effects(f, o):
    s = f.submission
    if s == None:
        return []
    # Заказ уже у администратора — решение за ним, скрипт больше не вмешивается.
    if s.escalated:
        return []

    if s.all_match:
        # Данные сошлись: это и есть подтверждение личности.
        return [verify_user(user_id = o.customer_id, order_id = o.id)] + _reward_effects(f, o)

    if s.attempt < MAX_ATTEMPTS:
        # Первое несовпадение — скорее всего опечатка или неверно прочитанное
        # поле. Что именно не сошлось, модератору не сообщается: иначе остальные
        # поля можно было бы подобрать перебором.
        return [system_message(order_id = o.id, text = MSG_CHECK_PASSPORT)]

    # Попытки исчерпаны. Дальше решает администратор: он видит, что вводил
    # модератор, и сверяет это с учётной записью сам.
    return [
        escalate(order_id = o.id, reason = REASON_ESCALATED),
        system_message(order_id = o.id, text = MSG_ESCALATED),
    ]

def on_event(f):
    o = f.order
    if o == None:
        return []
    # Закрыть верификацией можно только незавершённый заказ.
    if o.status != STATUS_ASSIGNED and o.status != STATUS_EXECUTED:
        return []

    if f.event == EVENT_ORDER_SUBMISSION:
        return _submission_effects(f, o)

    if f.event == EVENT_ORDER_EXECUTED:
        if f.config.get(CFG_VERIFIED_BY, VERIFIED_BY) != VERIFIED_BY_MODERATOR:
            # В этой конфигурации отметку ставит администратор: ждём события
            # user.verified, а не отчёт о выполнении.
            return []
        if f.customer == None or f.customer.is_verified:
            return []
        # Отметка «выполнено» сама по себе больше ничего не подтверждает:
        # личность подтверждает совпадение введённых данных (EVENT_ORDER_SUBMISSION).
        # Если модератор отметил выполнение, не пройдя проверку, — напоминаем.
        return [system_message(order_id = o.id, text = MSG_CHECK_PASSPORT)]

    if f.event == EVENT_USER_VERIFIED:
        return _reward_effects(f, o)

    return []
