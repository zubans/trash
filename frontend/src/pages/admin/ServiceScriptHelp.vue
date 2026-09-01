<template>
  <div class="script-help">
    <header class="help-header">
      <h1>Как писать скрипты услуг</h1>
      <p class="lead">
        Спец-услуга — это услуга, правила которой не выражаются галочками:
        кому она видна, сколько стоит, сколько раз её можно заказать и что
        произойдёт, когда её выполнят. Эти правила пишутся скриптом на
        <strong>Starlark</strong> (синтаксис Python) прямо в конструкторе услуг.
      </p>
    </header>

    <section class="help-block">
      <h2>Два поля</h2>
      <p>
        <strong>Константы и переменные</strong> — суммы, роли, имена событий,
        тексты сообщений. Всё, что кто-то захочет поменять, не читая логику.
        <strong>Скрипт услуги</strong> — <code>MANIFEST</code> и функции-хуки,
        которые эти константы используют. Константы выполняются первыми, поэтому
        в скрипте они доступны как обычные имена.
      </p>
      <p class="note">
        Скрипт компилируется при сохранении. Если он не компилируется, услуга не
        сохранится, а вы увидите ошибку с номером строки — сломанный скрипт
        никогда не попадает в базу.
      </p>
    </section>

    <section class="help-block">
      <h2>MANIFEST</h2>
      <p>Что ядру нужно знать, не запуская код.</p>
      <pre><code>MANIFEST = {
    "name": "Название поведения",
    "description": "Одна строка для админки",
    "once_per_user": True,            # услугу можно заказать один раз
    "release_claim_on_cancel": True,  # отмена возвращает попытку
    "events": ["order.executed"],     # на какие события реагируем
    "defaults": {"reward": 200},      # значения по умолчанию для настроек узла
}</code></pre>
    </section>

    <section class="help-block">
      <h2>Хуки</h2>
      <p>
        Определяйте только те, что нужны. Отсутствующий хук означает «нет
        мнения» — применяется обычное правило платформы. Скрипт может только
        <em>сузить</em> то, что платформа уже разрешила: снять бан или
        возрастное ограничение он не может.
      </p>
      <table class="help-table">
        <thead>
          <tr><th>Хук</th><th>Когда</th><th>Что вернуть</th></tr>
        </thead>
        <tbody>
          <tr><td><code>visible(f)</code></td><td>показ услуги в каталоге</td><td><code>True</code> / <code>False</code></td></tr>
          <tr><td><code>can_order(f)</code></td><td>создание заказа</td><td><code>None</code> — можно; строка — отказ с этим текстом</td></tr>
          <tr><td><code>can_view_or_take(f)</code></td><td>список, карта, принятие заказа</td><td>так же</td></tr>
          <tr><td><code>price(f)</code></td><td>расчёт цены</td><td>число (рубли) или <code>None</code></td></tr>
          <tr><td><code>on_event(f)</code></td><td>событие из MANIFEST</td><td>список эффектов</td></tr>
        </tbody>
      </table>
    </section>

    <section class="help-block">
      <h2>Что известно скрипту: <code>f</code></h2>
      <pre><code>f.event      имя события (только в on_event)
f.config     настройки узла поверх MANIFEST["defaults"]: f.config.get("reward", 0)
f.user       тот, о ком решение (заказчик), или None
f.viewer     исполнитель или модератор, который смотрит заказ, или None
f.customer   заказчик заказа
f.order      .id .status .customer_id .executor_id .amount .is_urgent .is_asap
f.variant    .id .code .base_price
f.claims     сколько раз f.user уже заказывал эту услугу
f.now        время, unix-секунды

has_role(actor, "MODERATOR")   проверка роли</code></pre>
      <p class="note">
        Скрипт не ходит в базу, не открывает сеть и не хранит состояние между
        вызовами. Он получает факты и возвращает решение — этим и ограничен
        ущерб от ошибки в нём.
      </p>
    </section>

    <section class="help-block">
      <h2>Эффекты</h2>
      <p><code>on_event</code> возвращает список того, что нужно сделать. Выполняет это платформа, одной транзакцией и со своими проверками.</p>
      <pre><code>complete_order(order_id = o.id, reason = "готово")
cancel_order(order_id = o.id, reason = "причина")
verify_user(user_id = o.customer_id, order_id = o.id)
system_message(order_id = o.id, text = "Сообщение в чат заказа")
escalate(order_id = o.id, reason = "почему это должен решать администратор")
pay_bonus(
    to = o.executor_id,          # только участник этого заказа
    amount = 200,                # рубли, не выше настройки behavior_max_bonus
    order_id = o.id,
    key = "мой_бонус:" + o.id,   # обязателен, иначе выплата повторится
    commission = False,          # удерживать ли комиссию платформы
)</code></pre>
      <p>
        <strong>Ключ выплаты обязателен.</strong> У платежа нет состояния,
        которое можно проверить: вторая оплата выглядит ровно как первая. Ключ
        должен быть привязан к заказу и к получателю — если платите и заказчику,
        и исполнителю, ключи должны отличаться.
      </p>
      <p>
        <strong>Комиссия по умолчанию не удерживается.</strong> Комиссия — это
        доля платформы от того, что заплатил заказчик; вознаграждение платит сама
        платформа. Включайте <code>commission = True</code> осознанно.
      </p>
    </section>

    <section class="help-block">
      <h2>Проверка данных исполнителем</h2>
      <p>
        Если в <code>MANIFEST</code> объявлены <code>check_fields</code>,
        приложение исполнителя показывает форму ровно под эти поля, а платформа
        сама сверяет введённое с учётной записью заказчика. Значения для сравнения
        исполнителю не отдаются — он их не видит и не может подсмотреть.
      </p>
      <pre><code>MANIFEST = {
    ...
    "check_fields": ["last_name", "first_name", "patronymic", "birth_date"],
    "hide_customer_contacts": True,   # исполнителю видно только адрес
    "events": ["order.submission"],
}</code></pre>
      <p>После отправки скрипт получает <strong>результат</strong> сравнения:</p>
      <pre><code>f.submission.attempt     номер попытки, начиная с 1
f.submission.all_match   всё ли совпало
f.submission.matches     по полям: {"last_name": True, "birth_date": False}
f.submission.escalated   заказ уже у администратора</code></pre>
      <p class="note">
        Сами данные аккаунта скрипту тоже не передаются — только совпало/не
        совпало. Не пишите в сообщение исполнителю, какое поле не сошлось:
        остальные тогда подбираются перебором.
      </p>
      <p>
        Типичная политика: первое несовпадение — предупреждение сверить с
        документом, последнее — <code>escalate</code>. Эскалация закрывает заказ
        для исполнителя (новые отправки отклоняются) и кладёт его на экран
        «Модерация проверок» вместе со всеми попытками.
      </p>
    </section>

    <section class="help-block">
      <h2>События</h2>
      <p>
        <code>order.created</code>, <code>order.accepted</code>,
        <code>order.executed</code>, <code>order.confirmed</code>,
        <code>order.canceled</code>, <code>user.verified</code>,
        <code>order.submission</code> (исполнитель отправил данные на проверку).
      </p>
      <p>
        События о заказе приходят поведению этого заказа; событие о пользователе
        (<code>user.verified</code>) — поведениям всех его незакрытых заказов.
        Событие обрабатывается фоновым обработчиком через несколько секунд после
        действия, а не мгновенно.
      </p>
      <p class="note">
        Обработка повторяется при сбое, поэтому <code>on_event</code> должен быть
        безопасен при повторном вызове: проверяйте статус заказа
        (<code>o.status</code>) и всегда указывайте ключ у выплат.
      </p>
    </section>

    <section class="help-block">
      <h2>Пример: услуга бесплатна и видна только новичкам</h2>
      <div class="example-grid">
        <div>
          <div class="example-label">Константы и переменные</div>
          <pre><code>REWARD = 100
MSG_ONLY_NEW = "услуга доступна только новым пользователям"</code></pre>
        </div>
        <div>
          <div class="example-label">Скрипт услуги</div>
          <pre><code>MANIFEST = {
    "name": "Пример",
    "once_per_user": True,
    "events": ["order.confirmed"],
}

def visible(f):
    return f.user != None and f.claims == 0

def can_order(f):
    if f.user == None or f.claims > 0:
        return MSG_ONLY_NEW
    return None

def price(f):
    return 0

def on_event(f):
    o = f.order
    if o == None or o.executor_id == None:
        return []
    return [pay_bonus(
        to = o.executor_id,
        amount = REWARD,
        order_id = o.id,
        key = "example:" + o.id,
    )]</code></pre>
        </div>
      </div>
    </section>

    <section class="help-block">
      <h2>Если что-то пошло не так</h2>
      <ul>
        <li>Услуга пропала из каталога — скрипт падает при выполнении. Хуки при ошибке закрываются: услуга не показывается и не заказывается. Смотрите метрику <code>behavior_hook_errors_total</code> и логи.</li>
        <li>Заказ не закрывается сам, вознаграждение не пришло — очередь событий не разбирается: метрика <code>behavior_events_pending</code>.</li>
        <li>Вознаграждение не выплатилось дважды при повторе — так и задумано: ключ идемпотентности сработал.</li>
      </ul>
    </section>

    <footer class="help-footer">
      Полное описание механизма — в документации проекта,
      <code>doc/service_behaviors.md</code>.
    </footer>
  </div>
</template>

<script lang="ts">
import { defineComponent } from 'vue'

export default defineComponent({
  name: 'ServiceScriptHelp',
})
</script>

<style scoped>
.script-help {
  max-width: 900px;
  margin: 0 auto;
  padding: 24px;
  color: #0f172a;
  line-height: 1.6;
}

.help-header h1 {
  font-size: 26px;
  font-weight: 700;
  margin: 0 0 12px;
}

.lead {
  color: #475569;
  margin: 0;
}

.help-block {
  margin-top: 32px;
}

.help-block h2 {
  font-size: 17px;
  font-weight: 700;
  margin: 0 0 10px;
}

.help-block p {
  margin: 0 0 12px;
}

.note {
  background: #f8fafc;
  border-left: 3px solid #6366f1;
  padding: 10px 14px;
  border-radius: 0 8px 8px 0;
  color: #475569;
  font-size: 14px;
}

pre {
  background: #0f172a;
  color: #e2e8f0;
  padding: 16px;
  border-radius: 12px;
  overflow-x: auto;
  font-size: 13px;
  line-height: 1.5;
}

code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

p code,
li code,
td code {
  background: #f1f5f9;
  padding: 2px 6px;
  border-radius: 6px;
  font-size: 13px;
}

.help-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

.help-table th,
.help-table td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid #e2e8f0;
  vertical-align: top;
}

.help-table th {
  color: #64748b;
  font-weight: 600;
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.4px;
}

.example-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.example-label {
  font-size: 12px;
  font-weight: 700;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  margin-bottom: 6px;
}

.help-footer {
  margin-top: 40px;
  padding-top: 16px;
  border-top: 1px solid #e2e8f0;
  color: #64748b;
  font-size: 14px;
}

@media (max-width: 760px) {
  .example-grid {
    grid-template-columns: 1fr;
  }
}
</style>
