# Изменения API для фронтенда

Сводка всего, что изменилось в API с момента исправления форм и деталей ошибок (#84) и до метрик (#105). Точные схемы:
[`openapi.yaml`](openapi.yaml), типы TypeScript: [`api-types.ts`](api-types.ts), правила обновления: [`api-contract.md`](api-contract.md),
коды ошибок для переводов: [`error-codes.md`](error-codes.md).

## 1. Что сломается, если ничего не менять

| Что | Было | Стало | Что сделать |
|---|---|---|---|
| Неверный пароль при `DELETE /profile` и `POST /profile/email` | 401 `INVALID_CREDENTIALS` | 400 `INVALID_CURRENT_PASSWORD` с `details[{field: "password", code: "INVALID_VALUE"}]` | 401 фронтенд трактует как «войти заново», поэтому раньше пользователя выбрасывало из аккаунта; теперь показывать ошибку у поля пароля. `POST /profile/password` отвечает так же, поле `current_password` |
| `assignees` и `tags` задачи | у `POST /lists/{id}/tasks` приходили `null` | всегда массив | убрать проверки на `null` |
| `options` и `config` пользовательского поля | иногда `null` | всегда массив и объект | то же |
| Права роли Admin | не мог менять рабочее пространство | `workspace.update` (имя, описание, аватар) есть у Owner и Admin | показывать форму Admin; удаление пространства только у Owner |
| `POST /profile/password` | возвращал токен | по-прежнему, но теперь завершает все остальные сеансы и шлёт письмо «Пароль изменён» | ничего, но объяснить пользователю про выход на других устройствах |
| Расписание проверки доступа в WebSocket | | при завершении сеанса пользователя (см. раздел 3) сервер закрывает его сокеты | уметь переподключаться по билету |

Остальные правки обратно совместимы: поля только добавлены.

## 2. Новые возможности

### Массовые операции над задачами

`POST /tasks/bulk`, тело `{action: "update"|"move"|"delete", task_ids: [..до 100], ...поля}`.

- `update`: `status_id`, `priority` (1-5), `due_date` (null снимает), `assignee_ids` (заменяют набор), `add_tag_ids`, `remove_tag_ids`; нужно хотя бы одно поле.
- `move`: `list_id` и, по желанию, `status_id`. `delete`: без других полей.
- Не атомарно. Ответ всегда 200: `{results: [{task_id, ok, code?, params?}], succeeded, failed}`. Показывать пользователю провалы
  по `code` (`FORBIDDEN`, `INVALID_TASK_REFERENCE`, `INVALID_DATE_RANGE`, `CONFLICTING_TAG_CHANGE`, `TASK_HAS_SUBTASKS`, `TASK_MOVE_CONFLICT`).
  Чужая, удалённая и несуществующая задача дают одинаковый `FORBIDDEN`.
- 400 `VALIDATION_FAILED` для кривого запроса; лимит 30 запросов в минуту.

### Сохранённые виды (фильтры и сортировка)

`GET/POST /workspaces/{id}/views`, `PATCH/DELETE /workspaces/{id}/views/{view_id}`. Вид: `name` (до 100 символов), `list_id` (или null),
`shared`, `filters` (те же параметры, что у `GET /lists/{id}/tasks`: `q`, `status_id`, `priority`, `tag_id`, `no_tags`, `assignee_id`,
`unassigned`, `created_*`, `due_*`, `start_*`, `has_due`, `parent_id`, `has_parent`, `has_open_checklist_items`), `sort`. Чтобы применить вид,
подставьте его `filters` и `sort` в запрос списка задач. Личный вид требует `task.read`, общий `space.update` (403 `VIEW_FORBIDDEN`);
до 200 видов на пользователя и пространство (409 `VIEW_LIMIT_REACHED`); неизвестный фильтр даёт 400 `INVALID_FILTER` с `filters.<имя>`.
В списке `is_owner` и `owner` (публичный профиль без почты) показывают, можно ли менять вид.

### Учёт времени

- `GET /tasks/{id}/time` → `{entries: [...], total_seconds}` (новые сверху, до 500). У записи `running` и, пока таймер идёт, текущее `seconds`.
- `POST /tasks/{id}/time` `{seconds 1..86400, started_at?, note?}` ручная запись; `POST /tasks/{id}/time/start` и `/stop` таймер.
  Один таймер на пользователя: 409 `TIMER_ALREADY_RUNNING`; остановка без таймера 404 `NO_RUNNING_TIMER`.
- `PATCH/DELETE /tasks/{id}/time/{entry_id}`: свою запись меняет автор, любую Owner и Admin (403 `TIME_ENTRY_FORBIDDEN`).
- `GET /time/running` → `{timer: null | {entry_id, task_id, task_title, started_at, seconds}}` для плашки «идёт таймер» в шапке.
- `GET /workspaces/{id}/time-report?from=&to=&user_id=` (UTC-даты, по умолчанию 30 дней, не больше 366): `{from, to, total_seconds, users: [{user, seconds, entries}]}`.
  Owner и Admin видят всех, остальные только себя.
- У задачи появилось `time_spent` (секунды, считается сервером по законченным записям).

### Зависимости между задачами

`GET/POST /tasks/{id}/relations`, `DELETE /tasks/{id}/relations/{other_id}?kind=`. Тело POST: `{task_id, kind: "blocks"|"blocked_by"|"related"}`.
Ответ: `{blocks, blocked_by, related, blocked}`, элементы `{id, title, list_id, status_id, done}`; `blocked` истинно, пока есть незавершённый
блокирующий (закрытие задачи не запрещается, флаг только для отображения). Ошибки: 400 `RELATION_SELF`, 404 `TASK_NOT_FOUND`,
409 `RELATION_EXISTS`, 409 `RELATION_CYCLE` (A блокирует B блокирует C блокирует A), 409 `RELATION_LIMIT_REACHED` (до 50 на задачу, `params.max`),
404 `RELATION_NOT_FOUND`. В ленте активности новые записи `relation.added` и `relation.removed`.

### Шаблоны списков

`GET /list-templates` (доступно любому вошедшему): `simple` (по умолчанию), `kanban`, `development`, `bugs`; имена и статусы локализованы по
`Accept-Language` (`ru*` русский, остальное английский). Выбранный `key` передавайте как `template` при создании пространства
(`POST /workspaces/{id}/spaces`) или списка; неизвестный ключ даёт 400 `UNKNOWN_LIST_TEMPLATE`. Статусы создаются на языке этого запроса и дальше
обычные данные, их можно переименовать.

### История изменений рабочего пространства

`GET /workspaces/{id}/audit-log?action=&page=&limit=` (Owner и Admin; по умолчанию 50, максимум 100). Записи: `workspace.updated`
(`changes.name` / `changes.description` с `from` и `to`), `workspace.avatar_changed`, `workspace.avatar_removed`, `member.added`,
`member.role_changed`, `member.removed`; у записи `actor` и `target` (публичные профили, без почты). Повторное переименование в то же имя
запись не создаёт.

### Сеансы и защита аккаунта

- `GET /profile/sessions` → `[{id, device, user_agent, ip, created_at, last_used_at, current}]`. `device` уже читаемый («Chrome on Windows»),
  `current` отмечает устройство этого запроса. `id` непрозрачная строка.
- `DELETE /profile/sessions/{id}` завершает один сеанс (чужой и несуществующий одинаково 404 `SESSION_NOT_FOUND`); если это текущий, сервер чистит cookie,
  а клиент должен перейти на вход. `DELETE /profile/sessions` завершает все остальные, ответ `{ended: N}`.
- Токен доступа завершённого устройства перестаёт работать сразу: ждите 401 и обрабатывайте как «войти заново». Новые права `Authorization` не нужны.
- Письма: «Новый вход» (вход с устройства, которого раньше не было; адрес в письме без последнего октета) и «Пароль изменён» (смена и сброс). Язык по
  `Accept-Language` запроса, поэтому отправляйте заголовок и при входе и при смене пароля.

## 3. Реалтайм

- События уведомлений и задач публикуются только после коммита, поэтому по событию `notification.created` запись всегда уже читается через REST.
- При завершении сеансов, выходе везде, смене или сбросе пароля, удалении аккаунта и удалении из пространства сервер закрывает сокеты пользователя.
  Получите новый билет (`POST .../events/ticket`) и подключитесь снова, если пользователь ещё авторизован.

## 4. Ограничения частоты и ошибки доставки

- Лимиты считаются в базе и общие для всех реплик: ответ 429 с заголовком `Retry-After` (секунды) приходит одинаково независимо от того, какая реплика приняла
  запрос. Показывайте паузу по `Retry-After`. Лимиты по IP: регистрация 5 в минуту, вход 10, обновление токена 30, выход 30, подтверждение почты 20,
  повторная отправка и «забыл пароль» 5, сброс пароля 10; `profile.sessions` 10 в минуту, `tasks.bulk` 30.
- Блокировка входа после серии неудач выглядит как обычное `INVALID_CREDENTIALS`: лишних сообщений о блокировке нет.
- Письма и webhook теперь ставятся в очередь и доходят с повторами (до 6 попыток), поэтому запросы, которые шлют письма (регистрация, смена почты,
  сброс пароля), отвечают быстро и не падают из-за недоступного SMTP.

## 5. Типы и проверка контракта

- Все успешные ответы описаны в OpenAPI, а каждый ответ интеграционных тестов сверяется со схемой. `docs/api-types.ts` генерируется командой `make api-types`.
- Использование: `import type { Endpoints } from "./api-types"; type Task = Endpoints["GET /tasks/{task_id}"]["data"];`. У каждой операции есть
  `params`, `query`, `body`, `status`, `data` и, для страничных списков, `pagination`.
- Списки с курсором (`GET /lists/{id}/tasks`) описаны как `TaskPage` (`items`, `has_next`, `has_prev`, `next_cursor?`, `prev_cursor?`, `total?`).
- Сообщения вида `{data: {message}}` у DELETE и подобных пока используют только как подтверждение; текст не показывайте, у клиента свои формулировки.

## 6. Новые коды ошибок для переводов

| Код | Где |
|---|---|
| `INVALID_CURRENT_PASSWORD` | неверный пароль (см. раздел 1) |
| `SESSION_NOT_FOUND`, `SESSION_LIST_FAILED`, `SESSION_REVOKE_FAILED` | сеансы |
| `VIEW_NOT_FOUND`, `VIEW_FORBIDDEN`, `VIEW_LIMIT_REACHED`, `INVALID_VIEW_ID`, `VIEW_FAILED`, `UNKNOWN_FILTER` | сохранённые виды |
| `TIMER_ALREADY_RUNNING`, `NO_RUNNING_TIMER`, `TIME_ENTRY_NOT_FOUND`, `TIME_ENTRY_FORBIDDEN`, `INVALID_TIME_ENTRY`, `INVALID_TIME_ENTRY_ID`, `TIME_FAILED` | учёт времени |
| `IN_THE_FUTURE`, `BEFORE_FROM`, `RANGE_TOO_LONG` | детали `VALIDATION_FAILED` в учёте времени и отчёте |
| `RELATION_SELF`, `RELATION_EXISTS`, `RELATION_CYCLE`, `RELATION_LIMIT_REACHED`, `RELATION_NOT_FOUND`, `RELATION_FAILED`, `SAME_TASK` | зависимости |
| `UNKNOWN_LIST_TEMPLATE` | шаблоны списков |
| `AUDIT_LOG_FETCH_FAILED` | история изменений |

## 7. Чек-лист миграции фронтенда

1. Перегенерировать клиент из `api-types.ts` и убрать проверки на `null` у `assignees`, `tags`, `options`, `config`.
2. Обработать 400 `INVALID_CURRENT_PASSWORD` у полей пароля вместо выхода из аккаунта.
3. Добавить переводы кодов из раздела 6.
4. Отправлять `Accept-Language` при входе, смене пароля и создании списков и пространств.
5. Страница «Сеансы» в настройках аккаунта (список, завершить, выйти на остальных).
6. Решить, какие из новых возможностей показывать сейчас: массовые операции, виды, время, зависимости, шаблоны списков, история изменений.
