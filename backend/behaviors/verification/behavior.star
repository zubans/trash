# Account verification.
#
# The service a customer orders to have their identity confirmed. Everything
# that makes it unusual lives here, not in the Go code:
#
#   * only an unverified user sees it, and only until they order it once;
#   * it is free;
#   * on the executor side only VERIFIER_ROLE may see or take it;
#   * when the customer becomes verified the order closes by itself and the
#     rewards in config.star are paid.
#
# The numbers, roles, event names and messages are all in config.star; this file
# is the logic that uses them. A node may override the amounts and the mode
# through service_nodes.behavior_config (the CFG_* keys).

MANIFEST = {
    "name": "Верификация аккаунта",
    "description": "Бесплатная услуга: модератор подтверждает личность пользователя и получает вознаграждение.",
    "once_per_user": True,
    "release_claim_on_cancel": True,
    "events": [EVENT_ORDER_EXECUTED, EVENT_USER_VERIFIED],
    "defaults": {
        CFG_REWARD_EXECUTOR: REWARD_EXECUTOR,
        CFG_REWARD_CUSTOMER: REWARD_CUSTOMER,
        CFG_APPLY_COMMISSION: APPLY_COMMISSION,
        CFG_VERIFIER_ROLE: VERIFIER_ROLE,
        CFG_VERIFIED_BY: VERIFIED_BY,
    },
}

# --- Catalog -----------------------------------------------------------------

def visible(f):
    # A verified user has nothing to order here, and neither has one who already
    # has a verification order running: claims counts the orders placed.
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
    # Free for the customer. The verifier is paid by the platform, not by them.
    return 0

# --- Executor side -----------------------------------------------------------

def can_view_or_take(f):
    role = f.config.get(CFG_VERIFIER_ROLE, VERIFIER_ROLE)
    if f.viewer == None or not has_role(f.viewer, role):
        return MSG_MODERATORS_ONLY
    if f.viewer.status == "BANNED":
        return MSG_BANNED
    if f.customer != None and f.viewer.id == f.customer.id:
        return MSG_SELF_VERIFICATION
    return None

# --- Reactions ---------------------------------------------------------------

def _reward(f, o, recipient, amount, role):
    # One reward, keyed by the order and by who it is for, so the same reward is
    # never paid twice however many events describe the same verification.
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

def on_event(f):
    o = f.order
    if o == None:
        return []
    # Only an order that is still running can be closed by a verification.
    if o.status != STATUS_ASSIGNED and o.status != STATUS_EXECUTED:
        return []

    if f.event == EVENT_ORDER_EXECUTED:
        if f.config.get(CFG_VERIFIED_BY, VERIFIED_BY) != VERIFIED_BY_MODERATOR:
            # The flag is an admin's decision in this configuration; wait for
            # the user.verified event instead of acting on the visit report.
            return []
        if f.customer == None or f.customer.is_verified:
            return []
        # verify_user is a request, not an act: the core applies it only when
        # the order was performed by somebody holding VERIFIER_ROLE.
        return [verify_user(user_id = o.customer_id, order_id = o.id)] + _reward_effects(f, o)

    if f.event == EVENT_USER_VERIFIED:
        return _reward_effects(f, o)

    return []
