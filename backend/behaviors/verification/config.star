# Everything the verification behaviour exchanges with the world outside the
# script: what it pays, who may perform it, which events and statuses it reacts
# to, and what its configuration keys are called.
#
# It lives in its own file so that changing a rule and changing a number are two
# different edits. behavior.star holds the logic and reads these names; nothing
# in it is a literal that an operator would want to change.
#
# Every amount below is also overridable per catalog node through
# service_nodes.behavior_config, under the CFG_* key of the same meaning — the
# constant here is the default the admin panel offers.

# --- Money ------------------------------------------------------------------

# Paid to the executor (the moderator) who performed the verification, in
# rubles, from the platform's BONUSES account.
REWARD_EXECUTOR = 200

# Paid to the customer who got verified. Zero by default: verification is
# already free, and a welcome bonus is a marketing decision, not a rule of the
# service. Set it here or per node to turn it on.
REWARD_CUSTOMER = 0

# Whether the platform's commission (order_commission_percent) is withheld from
# the rewards above.
#
# False, and deliberately so: the commission is the platform's share of what a
# customer paid, and nobody paid for a free service. Taking a cut of money the
# platform is itself paying out would only move it from one of its own accounts
# to another. A behaviour that does want its rewards treated as ordinary
# earnings sets this to True (or apply_commission on the node).
APPLY_COMMISSION = False

# --- Roles ------------------------------------------------------------------

# Who may see and take a verification order on the executor side. Read by the
# script and, independently, by the core when it decides whether to honour a
# verify_user effect — the script asks, the core checks.
VERIFIER_ROLE = "MODERATOR"

# --- Verification mode ------------------------------------------------------

# Who turns the customer's verified flag on:
#   "moderator" — the verifier marking the order done verifies them;
#   "admin"     — it stays an administrator's checkbox and the script only
#                 reacts to it.
VERIFIED_BY = "moderator"

VERIFIED_BY_MODERATOR = "moderator"
VERIFIED_BY_ADMIN = "admin"

# --- Node configuration keys -------------------------------------------------

CFG_REWARD_EXECUTOR = "reward_executor"
CFG_REWARD_CUSTOMER = "reward_customer"
CFG_APPLY_COMMISSION = "apply_commission"
CFG_VERIFIER_ROLE = "verifier_role"
CFG_VERIFIED_BY = "verified_by"

# --- The core's vocabulary ---------------------------------------------------
# Event names, order statuses and roles as the Go side spells them. Kept here so
# a rename on that side is one edit in one file per behaviour.

EVENT_ORDER_EXECUTED = "order.executed"
EVENT_USER_VERIFIED = "user.verified"

STATUS_ASSIGNED = "ASSIGNED"
STATUS_EXECUTED = "EXECUTED"

ROLE_CUSTOMER = "CUSTOMER"

# Prefix of the idempotency key that ties a reward to its order, so the same
# reward is never paid twice however many events describe the verification.
REWARD_KEY_PREFIX = "verification"

# --- Messages ----------------------------------------------------------------

MSG_ALREADY_VERIFIED = "ваш аккаунт уже подтверждён"
MSG_ALREADY_ORDERED = "услуга верификации уже была заказана"
MSG_ANONYMOUS = "услуга доступна только авторизованным пользователям"
MSG_MODERATORS_ONLY = "верификацию выполняют только модераторы"
MSG_BANNED = "аккаунт заблокирован"
MSG_SELF_VERIFICATION = "нельзя верифицировать самого себя"
MSG_ORDER_CLOSED = "✅ Аккаунт подтверждён. Заказ закрыт автоматически."
