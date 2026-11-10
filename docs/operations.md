# Эксплуатация

Runbook для запуска, обновления и разбора сбоев. Переменные окружения описаны в `.env.example`, почта в
[`mail.md`](mail.md), архитектура в [`architecture.md`](architecture.md).

## Окружения

| `APP_ENV` | Режим |
|---|---|
| `development` | Режим отладки gin, текстовые логи, полный текст писем в логе, запасной `JWT_SECRET` допустим |
| `test` | Для автотестов |
| `staging`, `production` | Режим release, JSON-логи, строгая проверка конфигурации, secure-cookie, `MAIL_DRIVER=log` запрещён |

Без `docker-compose.prod.yml` compose работает как `development`; не открывайте такой запуск наружу.

## Запуск

```bash
cp .env.example .env     # обязательно POSTGRES_PASSWORD и JWT_SECRET (не короче 32 символов)
docker compose up --build                                                   # разработка
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build   # прод
```

Порядок: `db` (healthcheck) -> `migrate` (`migrate -action=up`) -> `app`. Dev-override привязывает Postgres и API
к `127.0.0.1`.

Без Docker: `make run` (миграции и сервер), `make test`, `make lint`.

Профили compose: `--profile mail` (Mailpit на http://127.0.0.1:8025), `--profile s3` (локальный MinIO и бакет).

## Проверка конфигурации

Сервер и `cmd/migrate` проверяют всю конфигурацию при старте и завершаются с одним сообщением, где перечислены все
проблемы: переменная, что не так, как исправить. Значения секретов не печатаются. Типичные причины отказа:

- `JWT_SECRET` короче 32 символов или остался запасным вне development;
- `ALLOWED_ORIGINS` не https в staging и production, содержит путь или слэш в конце;
- `MAIL_DRIVER=log` в production, `FRONTEND_URL` или `PUBLIC_BASE_URL` не https в staging и production;
- `DB_LOCK_TIMEOUT` больше `DB_STATEMENT_TIMEOUT`;
- `TRUSTED_PROXIES` содержит `0.0.0.0/0` или `::/0`;
- `STORAGE_DRIVER=s3` без `S3_*`.

## Health

- `GET /healthz` — процесс жив.
- `GET /readyz` — ещё и ответ базы. Используйте для readiness-проб и балансировщика.

## Миграции

Файлы лежат в `db/migrations` и встроены в бинарник. Команды `cmd/migrate`:

| Команда | Действие |
|---|---|
| `-action up` | Накатить новые миграции |
| `-action down -steps N` | Откатить N шагов (без `-steps` отказ) |
| `-action down -all` | Откатить всё; запрещено при `APP_ENV=production` |
| `-action version` | Текущая версия |
| `-action force -version N` | Сбросить dirty-состояние на версию N |

Сервер при старте сверяет версию схемы и завершается, если база отстаёт или dirty. Лечение: `migrate -action up`;
если миграция упала посередине, исправьте причину, затем `force -version <последняя целая>` и повторите `up`.

До первого релиза `000001_init_schema` правится на месте; после правок локальную базу пересоздают:
`docker compose down -v`. С первого релиза файлы заморожены, любое изменение схемы только новым файлом
(`make db-create name=...`). CI проверяет круг `up -> down -> up`.

## Остановка

На SIGTERM сервер перестаёт принимать запросы и ждёт до `SHUTDOWN_TIMEOUT` (30 с), пока завершатся запросы и
текущая обработка воркеров outbox (почта, уведомления, webhook). Необработанное остаётся в базе и продолжится после запуска. Compose держит контейнер `stop_grace_period: 45s`; он должен быть
больше `SHUTDOWN_TIMEOUT`.

## За обратным прокси

- Поставьте TLS-прокси перед API. Установите `TRUSTED_PROXIES` на адреса прокси: только так `X-Forwarded-For`
  считается настоящим адресом клиента, иначе все клиенты выглядят как адрес прокси и делят лимиты.
- Пропускайте тела не меньше `MAX_ATTACHMENT_BYTES` (nginx: `client_max_body_size`).
- WebSocket (`/events`) требует передачи заголовков `Upgrade` и `Connection`.
- Задайте `PUBLIC_BASE_URL` публичным адресом API: от него зависят абсолютные URL картинок.
- Задайте `FRONTEND_URL`: на него указывают ссылки в письмах.

## Метрики и алерты

`GET /metrics` отдаёт метрики в формате Prometheus. В `staging` и `production` эндпоинт требует токен
(`METRICS_TOKEN`, не короче 16 символов, заголовок `Authorization: Bearer ...`), без него сервер не запустится; отключить
эндпоинт можно через `METRICS_ENABLED=false`. В development токен не нужен. Запросы к `/metrics` не попадают в журнал доступа
и не считаются в метриках.

```yaml
scrape_configs:
  - job_name: rabbit-hole
    metrics_path: /metrics
    authorization: {type: Bearer, credentials_file: /etc/prometheus/rabbit-hole.token}
    static_configs: [{targets: ["api.internal:8080"]}]
```

| Метрика | Что показывает |
|---|---|
| `rabbit_hole_http_requests_total{method,route,status}` | запросы; `route` это шаблон маршрута (`/api/v1/tasks/:task_id`), поэтому число рядов ограничено; неизвестные пути попадают в `unmatched` |
| `rabbit_hole_http_request_duration_seconds{method,route}` | гистограмма задержки (от 5 мс до 10 с) |
| `rabbit_hole_http_requests_in_flight` | запросы в работе сейчас |
| `rabbit_hole_outbox_pending{event_type}` | события очереди, ожидающие обработки (вместе с повторами) |
| `rabbit_hole_outbox_oldest_due_age_seconds{event_type}` | сколько ждёт самое старое событие, которое уже пора обработать; 0, если очередь справляется |
| `rabbit_hole_outbox_dead{event_type}` | события, исчерпавшие попытки |
| `rabbit_hole_outbox_events_processed_total{event_type,result}` | исходы обработки этим процессом: `ok`, `retry`, `dead` |
| `rabbit_hole_outbox_stats_up` | 1, если состояние очереди удалось прочитать |
| `go_sql_*{db_name="main"}` | пул соединений: открыто, занято, ожидания (`go_sql_wait_count_total`) |
| `rabbit_hole_websocket_connections` | открытые WebSocket этого процесса |
| `go_*`, `process_*` | рантайм Go и процесс |

Состояние очереди читается из базы не чаще раза в 10 секунд, сколько бы ни было сборщиков. Счётчики исходов и соединений считаются
на процесс, поэтому при нескольких репликах суммируйте их запросом (`sum(...)`); длина очереди одна на всех, берите `max`.

Правила оповещений лежат в [`alerts.yml`](alerts.yml): сервис не отвечает, доля 5xx выше 5 %, p95 выше секунды, поток
ответов 429, задержка очереди выше 5 минут, мёртвые события, недоступность статистики очереди, исчерпание пула соединений.
Пороги это отправная точка, подстройте их под свою нагрузку. Подключение: `rule_files: ["alerts.yml"]`.

Что делать по алертам:

| Алерт | Первый шаг |
|---|---|
| `OutboxBacklog` | `SELECT event_type, count(*) ... WHERE published_at IS NULL` (см. «Очередь outbox»), журнал `outbox: cannot process events`, живы ли воркеры |
| `OutboxDeadEvents` | `last_error` у строк с `dead_at`, исправить причину (SMTP, получатель webhook), вернуть строки в очередь |
| `HighServerErrorRate` | строки журнала с уровнем ERROR, разбивка `sum by (route)` |
| `DatabasePoolSaturated` | предупреждения о медленных запросах, затем `DB_MAX_OPEN_CONNS` |
| `TooManyRateLimited` | не задан `TRUSTED_PROXIES` (все клиенты на одном адресе) либо атака |

## Журналы

- `LOG_LEVEL`: debug, info, warn, error. По умолчанию debug в development и info в остальных.
- В логах нет значений параметров SQL, токенов, паролей и полных адресов e-mail.
- Каждый запрос получает `request_id` (он же в ответе об ошибке): по нему находят строки журнала доступа и
  ошибки.
- Медленные запросы дольше `DB_SLOW_QUERY_THRESHOLD` (200 мс) пишутся как предупреждения.
- Сообщения, на которые стоит смотреть: `outbox: event failed, will retry`, `outbox: giving up on an event`, `webhook: delivery failed`, предупреждения о запуске с `MAIL_DRIVER=log`.

## Лимиты и пределы

| Что | Значение |
|---|---|
| Тело запроса | 1 МиБ (публичная отправка формы 128 КиБ; вложения до `MAX_ATTACHMENT_BYTES`, картинки `MAX_IMAGE_BYTES`, аватары `MAX_AVATAR_BYTES`) |
| Соединения БД | `DB_MAX_OPEN_CONNS` 25, `DB_MAX_IDLE_CONNS` 10 |
| Таймауты сессии БД | `DB_STATEMENT_TIMEOUT` 30 с, `DB_LOCK_TIMEOUT` 5 с, `DB_IDLE_IN_TX_TIMEOUT` 1 мин |
| Хеширование паролей | `AUTH_HASH_CONCURRENCY` одновременных (по умолчанию число CPU), ожидание `AUTH_HASH_WAIT` |
| Почта | очередь outbox (6 попыток); `NOTIFICATION_EMAIL_PER_HOUR` на пользователя |
| Отправки форм | `FORM_SUBMISSIONS_PER_HOUR` на форму, дополнительно лимит по IP |
| Realtime | ограниченное число соединений на пользователя, билет живёт 30 с |
| Метрики | `METRICS_ENABLED` (true), `METRICS_TOKEN` (обязателен в staging и production) |

Лимиты запросов по IP, блокировка входа, лимит писем уведомлений и лимит отправок форм считаются в PostgreSQL
(`RATE_LIMIT_STORE=postgres`, по умолчанию): все реплики делят один бюджет, рестарт его не сбрасывает. Таблицы
`rate_limit_windows` и `login_lockouts` чистятся раз в 10 минут. При `RATE_LIMIT_STORE=memory` счётчики хранятся в
процессе (одна реплика, рестарт обнуляет). Если хранилище недоступно, запрос пропускается и пишется предупреждение
`rate limit: store unavailable`. Семафор argon2 всегда считается на процесс.

## Хранилище файлов

- `STORAGE_DRIVER=local`: каталог `STORAGE_LOCAL_DIR` (в compose `/data/uploads`); смонтируйте том и включите его
  в резервные копии.
- `STORAGE_DRIVER=s3`: MinIO или любой S3-совместимый бакет; бакет должен существовать заранее.
- Просроченные загрузки форм (`FORM_UPLOAD_TTL`) удаляет фоновая очистка раз в 10 минут.

## Резервные копии и восстановление

Нужны две вещи: дамп PostgreSQL (`pg_dump`) и каталог или бакет файлов. Восстанавливайте их из одного момента
времени: в таблице `files` хранятся ключи объектов. После восстановления запустите `migrate -action version` и
убедитесь, что версия совпадает с версией бинарника.

## Очередь outbox

Письма, уведомления и webhook проходят через таблицу `outbox_events`. Полезные запросы:

```sql
SELECT event_type, count(*) FROM outbox_events WHERE published_at IS NULL AND dead_at IS NULL GROUP BY 1;
SELECT id, event_type, attempts, last_error, dead_at FROM outbox_events WHERE dead_at IS NOT NULL ORDER BY id DESC LIMIT 20;
```

- Растущее число необработанных строк значит, что воркеры не успевают или остановлены (смотрите журнал
  `outbox: cannot process events`).
- Строка с `dead_at` исчерпала попытки. Исправив причину (например SMTP), верните её в очередь:
  `UPDATE outbox_events SET dead_at = NULL, attempts = 0, next_attempt_at = now() WHERE id = ...`. У писем
  тело удаляется только после успешной отправки, поэтому такая строка ещё пригодна.
- Старые строки удаляются фоновой чисткой (раздел «Хранение технических данных»).

## Хранение технических данных

Фоновое задание раз в `RETENTION_INTERVAL` (1 ч) удаляет пачками по 1000 строк:

| Данные | Условие удаления | Переменная (по умолчанию) |
|---|---|---|
| Обработанные события `outbox_events` | `published_at` старше срока | `RETENTION_OUTBOX_DONE` (168 ч) |
| Мёртвые события `outbox_events` | `dead_at` старше срока | `RETENTION_OUTBOX_DEAD` (720 ч) |
| `email_tokens`, `channel_confirmations`, `user_sessions`, `form_idempotency_keys` | истекли или использованы раньше срока | `RETENTION_EXPIRED_TOKENS` (168 ч) |

- `0` отключает правило. Необработанные события не удаляются никогда, как бы давно они ни лежали.
- Мёртвые события хранятся дольше обработанных, чтобы успеть разобрать сбой и вернуть строку в очередь.
- Чистка не трогает данные пользователей: задачи, историю активности и мягко удалённые объекты. Расписание `rate_limit_windows`
  и `login_lockouts` отдельное (каждые 10 минут).
- Результат пишется в журнал: `retention: removed old technical data`.

## Тесты в CI

Интеграционные тесты идут тремя параллельными заданиями (`Integration Tests (shard N/3)`): `scripts/shard-tests.sh` раздаёт тесты
по очереди из отсортированного списка имён, у каждого задания свой раннер и своя база, поэтому они не мешают друг другу. Запустить
одну долю локально: `go test -run "$(scripts/shard-tests.sh ./tests/integration 2 3)" ./tests/integration`. Число долей меняется в матрице
`strategy.matrix` файла `.github/workflows/go.yml`. Новые тесты попадают в доли сами. Итоговый `CI Status` ждёт все три.

## Типовые сбои

| Симптом | Причина и действие |
|---|---|
| Сервер не стартует, печатает список проблем | Исправьте перечисленные переменные, см. «Проверка конфигурации» |
| Сервер не стартует: схема отстаёт или dirty | `migrate -action up`; при dirty сначала разберите упавшую миграцию и `force` |
| 503 с `Retry-After` на входе | Заняты все слоты argon2; подождите, поднимите `AUTH_HASH_CONCURRENCY` под число CPU и память |
| 429 у всех пользователей сразу | Не задан `TRUSTED_PROXIES`, все клиенты выглядят одним адресом |
| Письма не приходят | Проверьте `MAIL_DRIVER`, `SMTP_*`, журнал `outbox: event failed, will retry`, см. [`mail.md`](mail.md) и раздел «Очередь outbox» |
| Webhook перестал приходить | Канал отключён после `NOTIFICATION_WEBHOOK_MAX_FAILURES` неудач; исправьте приёмник и подтвердите канал заново |
| WebSocket сразу закрывается | Прокси не передаёт `Upgrade`, билет просрочен (30 с) или пользователь вышел везде |
| Картинки ведут не на тот адрес | Не задан или неверен `PUBLIC_BASE_URL` |
| Таймауты запросов к БД | Смотрите предупреждения о медленных запросах и блокировки; `DB_LOCK_TIMEOUT` защищает от долгого ожидания |
| Отправка формы сохранена, задачи нет | Статус `failed`; повторите `POST /submissions/{id}/retry-task-creation` |

## Чек-лист перед первым релизом

- [ ] TLS-прокси перед API
- [ ] `TRUSTED_PROXIES` задан
- [ ] Отдельные роли БД для миграций и приложения вместо суперпользователя Postgres
- [ ] Публикация образа и закреплённые теги
- [ ] Обновить зависимости с исправлениями (`golang.org/x/net`)
- [ ] Заморозить миграции
- [ ] Настроены резервные копии БД и файлов
- [ ] Реальный SMTP проверен (`docs/mail.md`)
