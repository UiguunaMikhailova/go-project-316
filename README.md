### Hexlet tests and linter status:
[![Actions Status](https://github.com/UiguunaMikhailova/go-project-316/actions/workflows/hexlet-check.yml/badge.svg)](https://github.com/UiguunaMikhailova/go-project-316/actions)
[![ci](https://github.com/UiguunaMikhailova/go-project-316/actions/workflows/ci.yml/badge.svg)](https://github.com/UiguunaMikhailova/go-project-316/actions/workflows/ci.yml)

# hexlet-go-crawler

Консольный краулер на Go: обходит сайт, проверяет ссылки и статические файлы,
собирает SEO-метрики и формирует JSON-отчёт по каждой странице.

## Требования

* Go 1.25+
* make
* golangci-lint (опционально, для `make lint`)

## Установка

```bash
git clone https://github.com/UiguunaMikhailova/go-project-316.git
cd go-project-316
make install
make build
```

## Использование

```bash
# через собранный бинарник
bin/hexlet-go-crawler https://example.com

# через make
make run URL=https://example.com

# без сборки
go run ./cmd/hexlet-go-crawler https://example.com

# тонкая настройка обхода
bin/hexlet-go-crawler --depth 2 --workers 8 --rps 5 https://example.com
```

Если URL не указан, утилита печатает понятное сообщение и справку.

### Опции

| Опция | Значение по умолчанию | Описание |
| --- | --- | --- |
| `--depth` | `10` | глубина обхода |
| `--retries` | `1` | число повторных попыток для неудачных запросов |
| `--delay` | `0s` | пауза между запросами (например, `200ms`, `1s`) |
| `--timeout` | `15s` | таймаут одного запроса |
| `--rps` | `0` | лимит запросов в секунду (имеет приоритет над `--delay`) |
| `--user-agent` | — | собственный User-Agent |
| `--workers` | `4` | число параллельных воркеров |

## Пример отчёта

```json
{
  "root_url": "https://example.com",
  "depth": 1,
  "generated_at": "2024-05-18T12:34:56Z",
  "pages": [
    {
      "url": "https://example.com",
      "depth": 0,
      "http_status": 200,
      "status": "ok",
      "error": "",
      "broken_links": [
        {
          "url": "https://example.com/assets/ghost.css",
          "status_code": 404
        },
        {
          "url": "https://cdn.example.com/app.js",
          "error": "Get \"https://cdn.example.com/app.js\": dial tcp: lookup cdn.example.com: no such host"
        }
      ],
      "discovered_at": "2024-05-18T12:34:56Z"
    }
  ]
}
```

В `broken_links` попадают только недоступные ссылки — ответ 4xx/5xx или сетевая ошибка.

Сетевые ошибки не роняют утилиту: они попадают в поля `status` и `error`
соответствующей страницы, а код выхода остаётся `0`.

## Команды make

| Команда | Описание |
| --- | --- |
| `make install` | скачать зависимости |
| `make build` | собрать бинарник в `bin/hexlet-go-crawler` |
| `make run URL=<адрес>` | запустить обход указанного сайта |
| `make test` | прогнать тесты с детектором гонок |
| `make lint` | проверить код golangci-lint |
| `make fmt` | отформатировать код |
| `make clean` | удалить каталог `bin` |
