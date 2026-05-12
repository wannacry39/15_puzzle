# 15-Puzzle Multi-Agent

Распределенный Go-проект для решения игры «Пятнашки» с двумя LLM-агентами.
Оркестратор хранит состояние игры и предоставляет MCP-инструменты, а агенты
игрока и проверяющего вызывают эти инструменты через tool-calling цикл LLM.

## Архитектура

```text
Orchestrator :8080
  - REST API для жизненного цикла игры
  - MCP Streamable HTTP сервер на /mcp
  - состояние доски, валидация, правила ходов, игровой цикл

Player Agent :8081
  - REST endpoint /invoke
  - просит LLM вызвать get_state и move
  - возвращает один успешный ход

Checker Agent :8082
  - REST endpoint /invoke
  - просит LLM вызвать get_state
  - возвращает признак, решена ли головоломка
```

Поток выполнения:

```text
POST /game/start
      |
      v
игровой цикл orchestrator
      |
      +-- REST --> player-agent /invoke
      |              |
      |              +-- MCP --> orchestrator /mcp: get_state, move
      |
      +-- REST --> checker-agent /invoke
                     |
                     +-- MCP --> orchestrator /mcp: get_state
```

## Сервисы

### orchestrator

Задачи сервиса:

- хранит активные игры в памяти;
- валидирует доски и отклоняет нерешаемые позиции;
- проверяет корректность ходов;
- предоставляет MCP-инструменты через Streamable HTTP;
- вызывает агента игрока и агента проверки, пока головоломка не решена или не
  достигнут лимит `MAX_STEPS`.

REST endpoints:

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/health` | Проверка состояния сервиса |
| `POST` | `/game/start` | Запускает игру с переданной доской из 16 значений |
| `GET` | `/game/{gameId}/result` | Возвращает `gameId`, `solved` и `totalSteps` |

MCP-инструменты:

| Инструмент | Аргументы | Описание |
|---|---|---|
| `get_state` | нет | Возвращает `board`, форматированное поле `grid`, `step`, `gameId` и `last_moved` |
| `move` | число `tile` | Сдвигает плитку в пустую клетку, если ход допустим |

### player-agent

Агент игрока получает `POST /invoke`, формирует chat-запрос к OpenRouter и
позволяет модели самой выбрать MCP-инструменты. Агент выполняет tool calls до
тех пор, пока модель не сделает один валидный `move` или пока не будет достигнут
локальный лимит итераций.

Пример успешного ответа:

```json
{
  "tile": 5,
  "board": [1, 2, 3, 4, 5, 6, 7, 8, 0, 10, 11, 12, 9, 13, 14, 15]
}
```

### checker-agent

Агент проверки получает `POST /invoke`, просит модель посмотреть текущее
состояние через `get_state` и возвращает, совпадает ли доска с финальным
состоянием.

Пример ответа:

```json
{
  "solved": false,
  "gameId": "game-...",
  "step": 1
}
```

## Правила игры

Финальная доска:

```text
  1  2  3  4
  5  6  7  8
  9 10 11 12
 13 14 15  _
```

Доска должна содержать ровно значения `0..15`, где `0` обозначает пустую
клетку. Оркестратор применяет стандартное правило разрешимости для поля 4x4 и
отклоняет невозможные позиции.

Проверка ходов:

- можно двигать только плитки `1..15`;
- плитка должна быть соседней с пустой клеткой;
- плитку из `last_moved` нельзя сразу двигать повторно, чтобы не отменять
  предыдущий ход.

## Запуск через Docker

Требования:

- Docker с Docker Compose;
- OpenRouter API key в переменной `OPENROUTER_API_KEY`.

Создайте env-файл или экспортируйте переменную:

```bash
export OPENROUTER_API_KEY=sk-or-...
```

Запустите все сервисы:

```bash
docker compose up --build
```

Compose поднимает:

- orchestrator на `http://localhost:8080`;
- player agent на `http://localhost:8081`;
- checker agent на `http://localhost:8082`.

## Примеры запросов

Запуск простой игры:

```bash
curl -X POST http://localhost:8080/game/start \
  -H 'Content-Type: application/json' \
  -d '{"board":[1,2,3,4,0,6,7,8,5,10,11,12,9,13,14,15]}'
```

Запуск более сложной игры:

```bash
curl -X POST http://localhost:8080/game/start \
  -H 'Content-Type: application/json' \
  -d '{"board":[1,6,2,3,5,10,8,4,9,14,7,12,13,0,11,15]}'
```

Получение результата:

```bash
curl http://localhost:8080/game/<gameId>/result
```

Пример результата:

```json
{
  "gameId": "game-...",
  "solved": true,
  "totalSteps": 5
}
```

## Конфигурация

Конфигурация задается через переменные окружения. Значения по умолчанию для
проекта указаны в Docker Compose.

| Переменная | Сервис | Значение в Compose | Описание |
|---|---|---|---|
| `PORT` | все | `8080`, `8081`, `8082` | HTTP-порт внутри контейнера |
| `PLAYER_URL` | orchestrator | `http://player-agent:8081` | URL агента игрока |
| `CHECKER_URL` | orchestrator | `http://checker-agent:8082` | URL агента проверки |
| `MAX_STEPS` | orchestrator | `200` | Максимальное число ходов игрока |
| `STEP_TIMEOUT_MS` | orchestrator | `30000` | Таймаут каждого вызова агента |
| `STEP_DELAY_MS` | orchestrator | `5000` | Пауза между вызовом игрока и проверяющего |
| `ORCHESTRATOR_MCP_URL` | player, checker | `http://orchestrator:8080/mcp` | MCP endpoint для агентов |
| `OPENROUTER_API_KEY` | player, checker | обязательно | API key OpenRouter |
| `OPENROUTER_MODEL` | player, checker | `google/gemini-3.1-flash-lite` | ID модели OpenRouter |
| `OPENROUTER_TIMEOUT_MS` | player | `30000` | Таймаут LLM-запроса |
| `OPENROUTER_TIMEOUT_MS` | checker | `15000` | Таймаут LLM-запроса |
| `LOG_LEVEL` | все | `info` | Уровень логирования zerolog |

Примечание: в Go-коде значение `OPENROUTER_MODEL` по умолчанию равно
`deepseek/deepseek-chat`, если переменная не задана. Docker Compose переопределяет
его на `google/gemini-3.1-flash-lite`.

## Технологии

- Go `1.23.0` module с Go toolchain `1.24.5`;
- `github.com/mark3labs/mcp-go` для MCP Streamable HTTP;
- `github.com/sashabaranov/go-openai` как OpenAI-compatible клиент для OpenRouter;
- `github.com/rs/zerolog` для JSON-логов;
- `github.com/google/uuid` для ID игр.

## Структура проекта

```text
.
|-- checker-agent/
|   |-- api/
|   |-- llm/
|   |-- mcp/
|   |-- Dockerfile
|   `-- main.go
|-- orchestrator/
|   |-- api/
|   |-- client/
|   |-- game/
|   |-- mcp/
|   |-- Dockerfile
|   `-- main.go
|-- player-agent/
|   |-- api/
|   |-- llm/
|   |-- mcp/
|   |-- Dockerfile
|   `-- main.go
|-- .env.example
|-- docker-compose.yml
|-- go.mod
`-- go.sum
```

В репозитории используется один Go module. Каждый Dockerfile собирает отдельный
сервис из общего root build context.
