# ☁️ Personal Cloud

A self-hosted personal cloud for your own devices, built with Go, React and TypeScript — a simple local file manager for accessing your files from a browser over your own network.

Current version: `v0.7.0`  
Status: `v0.7.0` adds Preview & Search: a file preview window for images, video, audio, PDF and text, plus advanced search with filters, ranking and search inside text files — while retaining Cloud Management, secure device management and Tailscale access.  
Author: @MazZzoxa

🇬🇧 English · 🇷🇺 Русский

* * *

## ☁️ Personal Cloud

### About

Personal Cloud is a self-hosted personal cloud and file manager designed to run on your own home PC.

The main principle is simple: your files stay on your own computer, while your own devices can access them through a web browser over the local network. The project is intentionally designed around personal use and your own devices rather than a public multi-user cloud service.

### Features — v0.7.0

File storage and management

  * Local filesystem storage on the host PC
  * SQLite database for file metadata
  * File and folder listing
  * Upload and download
  * Create folders
  * Delete files and folders
  * Rename files and folders
  * Move files and folders between existing folders
  * Recursive copy of files and folders
  * Global search by file name, path, MIME type and extension
  * Extended metadata including size, MIME type, extension, creation time and modification time

Transfers

  * Streaming multipart uploads on the Go backend
  * Per-file upload progress in the browser
  * Streaming HTTP downloads with byte-range support
  * Correct download content type and content length handling

Interface

  * Responsive React interface for desktop and mobile
  * Breadcrumb navigation
  * Search with live results
  * Per-item action menus
  * Multi-selection with bulk file actions
  * Double-click folder opening on desktop
  * Upload progress panel
  * Drag-and-drop uploads on desktop
  * Mobile navigation drawer
  * Dedicated Personal Chat view

Personal Chat

  * Persistent message history in SQLite
  * Realtime message updates through WebSocket
  * Automatic WebSocket reconnect in the browser
  * History pagination with older-message loading
  * File attachments up to 100 MB per file and 8 files per message
  * Inline previews for raster images
  * Clickable HTTP/HTTPS links
  * Image previews for direct image links
  * Message selection with contextual actions
  * Shift-click range selection for consecutive messages
  * Copy selected message text
  * Download selected messages and attachments as one ZIP archive
  * Edit and delete selected messages

Secure devices (v0.4.0)

  * Every API route, file download, chat attachment and the chat WebSocket require a trusted device
  * The host PC (direct `localhost` connection) is trusted automatically
  * Other devices are paired once with a single-use code (8 characters, valid for 5 minutes)
  * Pairing link with the code in the URL fragment (`/#pair=XXXX-XXXX`) for one-tap pairing
  * Pairing code is also printed in the server console on startup
  * Device token stored only as a SHA-256 hash; the browser keeps it in an `HttpOnly` cookie
  * "Devices" view: list, online status, last activity, rename, revoke
  * Revoking a device disconnects its live WebSocket connections immediately
  * Brute-force protection for pairing (per-IP and global attempt limits)
  * CSRF protection (same-origin check) and same-origin WebSocket check

Remote access (v0.5.0)

  * Tailscale IPv4 detection through the Tailscale CLI with network-interface fallback
  * Remote access through the Tailscale network using the same Personal Cloud server port
  * Separate LAN and Tailscale routes exposed by `GET /api/network`
  * LAN / Tailscale connection switcher in the web interface
  * Pairing links are generated for available LAN and Tailscale routes
  * Tailscale connections still require a trusted Personal Cloud device
  * The host server does not automatically configure Tailscale Serve or Funnel

Cloud Management (v0.6.0)

  * Dedicated management views for trusted devices, storage, settings and event logs
  * Storage dashboard with file and folder counts, cloud data size, chat attachment size and SQLite database size
  * Host-volume capacity and free space where the operating system exposes it
  * Persistent cloud name, per-file upload limit and log retention settings stored in SQLite
  * Server-side enforcement of the configured file upload size limit
  * Audit log for file operations, chat changes, device pairing/access changes and configuration updates
  * Log filtering by severity and text, pagination, manual clearing and automatic retention cleanup

Search and preview (v0.7.0)

  * Multi-word search: every word has to match the name, path, extension or type
  * "Quoted phrases", `ext:pdf` and `type:image` shortcuts inside the query
  * Results ranked by relevance (name matches first); `ё` and `е` are treated as the same letter
  * Filters by file type and modification period, scope "only in the current folder", sorting by relevance, name, size, date or type
  * Optional search inside text and code files with a snippet and line number
  * Match highlighting, stale-request protection and a `/` shortcut to focus the search box
  * File preview window for images, video, audio, PDF and text/code files with previous/next navigation (← / →)
  * Text preview detects UTF-8, UTF-16 and Windows-1251 and shows the first 512 KB
  * Preview is served from an allow-list of safe types with a script-less sandbox (`Content-Security-Policy`), and supports HTTP range requests for seeking in media

Local networking

  * HTTP server available on the LAN through `0.0.0.0:8080`
  * LAN address printed when the server starts
  * Access from Windows, Android or another browser on the same network

Security foundation

  * Basic file path validation against directory traversal
  * File operations reject destinations outside the storage root
  * Chat attachment paths are generated server-side and are not addressable as arbitrary filesystem paths
  * Files remain on the host machine instead of being uploaded to a third-party cloud
  * Device authentication and trusted devices (v0.4.0)

### Not included yet

  * Automatic file synchronization
  * Thumbnails and Office document preview
  * Persistent full-text search index
  * Backup and recovery tools
  * Windows Service / automatic startup

### Tech stack

  * Backend: Go
  * HTTP: Go standard library (`net/http`)
  * Database: SQLite (`modernc.org/sqlite`)
  * File storage: local filesystem
  * Realtime transport: WebSocket (`github.com/gorilla/websocket`)
  * Frontend: React
  * Language: TypeScript
  * Build tool: Vite
  * Client: web browser
  * Remote networking: Tailscale

### Getting started

Requires:

  * Go `1.27.x`
  * Node.js + npm
  * Windows for the provided `.bat` scripts

Go `1.27.1` is the tested target for the current release line. The frontend uses React `19.3.0` and Vite `8.0.0`.

Run from the repository root:

```powershell
.\run.bat
```

Open Personal Cloud on the host PC:

```text
http://localhost:8080
```

The application creates these directories automatically:

```text
data/cloud.db
data/chat-attachments/
storage/
```

To access it from another device on the same LAN, use the LAN URL printed by the server, for example:

```text
LAN: http://192.168.1.50:8080
```

### Connecting devices (v0.4.0)

1. Open Personal Cloud on the host PC at `http://localhost:8080` — it is trusted automatically.
2. Go to **Devices → Add device**. A single-use code and a link are shown for 5 minutes.
3. On the phone or another PC open the LAN link (or the LAN URL and enter the code), name the device and connect.
4. A paired device stays trusted until you revoke it in the **Devices** view.

If no trusted device is available, use the pairing code printed in the server console at startup (valid for 10 minutes).

Set the environment variable `PC_TRUST_LOCALHOST=0` to require pairing even on the host PC. Requests that arrive through a proxy (with `X-Forwarded-*` headers) are never treated as the host.

### Remote access with Tailscale (v0.5.0)

1. Install and sign in to Tailscale on the host PC.
2. Install and sign in to Tailscale on the remote phone or PC using the same tailnet.
3. Start Personal Cloud normally. The server console prints a `Tailscale:` URL when a Tailscale IPv4 address is detected.
4. Open that Tailscale URL from the remote device. The connection is still protected by Personal Cloud device pairing and authentication.
5. The web interface also shows the detected LAN and Tailscale routes. Use the corresponding button to switch between them.

Tailscale provides the remote network path; Personal Cloud continues to listen on `0.0.0.0:8080`. The v0.5.0 implementation does not configure Tailscale Serve or Funnel automatically.

### Development mode

Backend:

```powershell
cd server
go run ./cmd/server
```

Frontend in another terminal:

```powershell
cd web
npm install
npm run dev -- --host
```

Vite serves the frontend while API requests are proxied to the Go server. Proxied requests carry `X-Forwarded-*` headers, so in development mode the browser is not the host: pair it with the code from the server console.

### Building

```powershell
cd web
npm install
npm run build
cd ..

cd server
go mod tidy
go build -o ..\personal-cloud.exe .\cmd\server
```

Or run:

```powershell
.\build.bat
```

### HTTP API

Core:

```text
GET    /api/health
GET    /api/info
GET    /api/files
POST   /api/files
GET    /api/files/download
DELETE /api/files
POST   /api/folders
```

v0.2.0 file management:

```text
PATCH  /api/files/rename
POST   /api/files/move
POST   /api/files/copy
GET    /api/search?q=<query>
```

v0.7.0 search and preview:

```text
GET    /api/search?q=<query>&type=<type>&modified=<period>&sort=<key>&order=<asc|desc>&content=1&path=<folder>&limit=<n>
GET    /api/files/preview?path=<file>        inline image / video / audio / PDF (supports Range)
GET    /api/files/preview/text?path=<file>   first 512 KB of a text file as UTF-8 JSON
```

Search parameters: `type` = `all`, `folder`, `image`, `video`, `audio`, `document`, `text`, `code`, `archive`, `other`; `modified` = `any`, `day`, `week`, `month`, `year`; `sort` = `relevance`, `name`, `size`, `modified`, `type`. The response contains `items`, `terms` (for highlighting), `total`, `truncated` and `partial`.

v0.5.0 network and v0.4.0 device APIs (public routes are marked; everything else requires a trusted device):

```text
GET    /api/health                  (public)
GET    /api/auth/me                 (public)
GET    /api/network                 (public)   LAN / Tailscale connection routes
POST   /api/auth/pair               (public, rate limited)  {"code":"ABCD-EFGH","name":"My phone"}
POST   /api/auth/logout
GET    /api/devices
POST   /api/devices/pairing         creates a single-use code and returns available routes
PATCH  /api/devices/<id>            {"name":"New name"}
DELETE /api/devices/<id>            revokes access
```

Cloud Management (v0.6.0; all routes require a trusted device):

```text
GET    /api/storage                 storage and host-volume metrics
GET    /api/settings                persisted cloud settings
PATCH  /api/settings                update cloudName, maxUploadMB, logRetentionDays
GET    /api/logs?limit=<n>&before=<id>&level=<level>&q=<query>
DELETE /api/logs                    clear the event log
```

Chat:

```text
GET    /api/chat/messages?limit=<n>&before=<message-id>
POST   /api/chat/messages
GET    /api/chat/ws
GET    /api/chat/attachments/<id>
```

`POST /api/chat/messages` accepts either JSON (`{"body":"..."}`) or `multipart/form-data` with a `body` field and one or more `files` fields.

Rename payload:

```json
{"path":"docs/report.pdf","name":"final-report.pdf"}
```

Move/copy payload:

```json
{"path":"docs/report.pdf","destination":"archive"}
```

Search examples:

```text
/api/search?q=report
/api/search?q=annual%20report&type=document&sort=modified
/api/search?q=TODO&content=1&type=code
/api/search?q=%22exact%20phrase%22%20ext:txt
```

### Project structure

```text
personal-cloud/
├── server/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── api/
│   │   ├── auth/
│   │   ├── chat/
│   │   ├── config/
│   │   ├── database/
│   │   ├── files/
│   │   ├── network/
│   │   └── storage/
│   └── go.mod
├── web/
│   ├── src/main.tsx
│   ├── src/styles.css
│   ├── index.html
│   └── package.json
├── data/
│   ├── cloud.db
│   └── chat-attachments/
├── storage/
├── docs/
├── README.md
├── RELEASE_NOTES_v0.2.0.md
├── RELEASE_NOTES_v0.3.0.md
├── RELEASE_NOTES_v0.4.0.md
├── RELEASE_NOTES_v0.5.0.md
├── RELEASE_NOTES_v0.6.0.md
├── RELEASE_NOTES_v0.7.0.md
├── VERSION
├── build.bat
└── run.bat
```

### Roadmap

The project follows a local storage → file manager → chat → secure devices → remote access → cloud management → preview/search → backup progression.

Version | Milestone
--- | ---
`v0.1.0` ✅ | Local Cloud — Go server, SQLite foundation, filesystem storage, web UI, LAN, upload/download
`v0.2.0` ✅ | File Manager — rename, move, copy, search, extended metadata, streaming and progress
`v0.3.0` ✅ | Personal Chat — messages, history, WebSocket, realtime, links, images and file attachments
`v0.4.0` ✅ | Secure Devices — device identity, pairing, authorization, trusted devices and sessions
`v0.5.0` ✅ | Remote Cloud — Tailscale access, remote connectivity and LAN / Tailscale route switching
`v0.6.0` ✅ | Cloud Management — device administration, storage metrics, persistent settings, audit log and configuration
`v0.7.0` ✅ | Preview & Search — advanced search, search inside text files and file preview
`v0.8.0` | Backup & Recovery — backup, restore, integrity checks and recovery tools
`v1.0.0` | Personal Cloud — complete core product

Full detailed design document: `docs/project-plan.md`.

### Contributing

Issues and pull requests are welcome. Please keep changes consistent with the self-hosted architecture and the project plan in `docs/`.

### License

MIT — use and modify the project freely while keeping the license notice.

* * *

## 🇷🇺 Русский

### О проекте

Personal Cloud — это личное self-hosted облако и файловый менеджер, рассчитанный на работу на домашнем ПК.

Главный принцип простой: файлы остаются на вашем компьютере, а ваши устройства получают к ним доступ через браузер по локальной сети. Проект изначально ориентирован на личное использование и собственные устройства, а не на публичный многопользовательский облачный сервис.

### Возможности — v0.7.0

Хранение и управление файлами

  * Локальное хранение файлов на компьютере-хосте
  * SQLite-база для метаданных файлов
  * Просмотр файлов и папок
  * Загрузка и скачивание файлов
  * Создание папок
  * Удаление файлов и папок
  * Переименование файлов и папок
  * Перемещение файлов и папок между существующими папками
  * Рекурсивное копирование файлов и папок
  * Глобальный поиск по имени, пути, MIME-типу и расширению
  * Расширенные метаданные: размер, MIME-тип, расширение, дата создания и изменения

Передача файлов

  * Потоковая multipart-загрузка на Go-сервере
  * Отображение прогресса загрузки для каждого файла
  * Потоковое HTTP-скачивание с поддержкой byte-range
  * Корректная передача MIME-типа и размера скачиваемого файла

Интерфейс

  * Адаптивный React-интерфейс для ПК и мобильных устройств
  * Навигация по пути через breadcrumbs
  * Поиск с отображением результатов в реальном времени
  * Меню действий для отдельных объектов
  * Множественное выделение и массовые действия с файлами
  * Открытие папок двойным щелчком на ПК
  * Панель прогресса загрузок
  * Drag-and-drop загрузка на ПК
  * Мобильное боковое меню навигации
  * Отдельный раздел «Чат»

Personal Chat

  * Постоянная история сообщений в SQLite
  * Обновление сообщений в реальном времени через WebSocket
  * Автоматическое переподключение WebSocket
  * Загрузка старых сообщений из истории
  * Вложения до 100 МБ на файл и до 8 файлов в одном сообщении
  * Предпросмотр PNG, JPEG, GIF, WebP и AVIF
  * Кликабельные HTTP/HTTPS-ссылки
  * Предпросмотр изображений по прямым ссылкам
  * Выделение сообщений с контекстной панелью действий
  * Выделение диапазона сообщений через Shift + клик
  * Копирование текста выделенных сообщений
  * Скачивание выделенных сообщений и вложений одним ZIP-архивом
  * Изменение и удаление выбранных сообщений

Защищённые устройства (v0.4.0)

  * Все API-методы, скачивание файлов, вложения чата и WebSocket чата требуют доверенного устройства
  * Компьютер-хост (прямое подключение через `localhost`) доверяется автоматически
  * Остальные устройства подключаются один раз по одноразовому коду (8 символов, действует 5 минут)
  * Ссылка подключения с кодом во фрагменте URL (`/#pair=XXXX-XXXX`) для подключения в одно касание
  * Код подключения также выводится в консоли сервера при запуске
  * Токен устройства хранится на сервере только в виде SHA-256 хеша; браузер держит его в `HttpOnly` cookie
  * Раздел «Устройства»: список, статус «в сети», последняя активность, переименование, отзыв доступа
  * Отзыв доступа сразу разрывает активные WebSocket-соединения устройства
  * Защита от перебора кода (лимиты попыток по IP и общий)
  * Защита от CSRF (проверка same-origin) и проверка origin для WebSocket

Удалённый доступ (v0.5.0)

  * Обнаружение Tailscale IPv4 через CLI Tailscale с fallback по сетевым интерфейсам
  * Удалённый доступ через Tailscale с использованием того же порта Personal Cloud
  * Отдельные маршруты LAN и Tailscale через `GET /api/network`
  * Переключатель маршрута LAN / Tailscale в web-интерфейсе
  * Ссылки pairing генерируются для доступных LAN- и Tailscale-маршрутов
  * Для Tailscale-подключений по-прежнему требуется доверенное устройство Personal Cloud
  * Приложение не настраивает Tailscale Serve или Funnel автоматически

Управление облаком (v0.6.0)

  * Отдельные разделы управления устройствами, хранилищем, настройками и журналом событий
  * Сводка по количеству файлов и папок, объёму файлов, вложений чата и SQLite-базы
  * Отображение общей ёмкости и свободного места диска, если ОС предоставляет эти сведения
  * Сохранение названия облака, лимита загрузки файла и срока хранения журналов в SQLite
  * Проверка лимита размера файла на стороне Go-сервера
  * Журнал операций с файлами, изменений чата, действий с устройствами и настроек
  * Фильтрация журнала по уровню и тексту, загрузка старых записей, очистка и автоматическое удаление устаревших событий

Поиск и предпросмотр (v0.7.0)

  * Поиск по нескольким словам: каждое слово должно встретиться в имени, пути, расширении или типе
  * «Фразы в кавычках», быстрые фильтры `ext:pdf` и `type:image` прямо в запросе
  * Ранжирование по релевантности (сначала совпадения в имени); `ё` и `е` считаются одной буквой
  * Фильтры по типу файла и периоду изменения, область «только в текущей папке», сортировка по релевантности, имени, размеру, дате и типу
  * Необязательный поиск внутри текстовых файлов и кода с фрагментом и номером строки
  * Подсветка совпадений, защита от устаревших ответов и клавиша `/` для перехода в строку поиска
  * Окно предпросмотра изображений, видео, аудио, PDF и текстовых файлов с кодом, переключение между файлами (← / →)
  * Текстовый предпросмотр распознаёт UTF-8, UTF-16 и Windows-1251 и показывает первые 512 КБ
  * Предпросмотр отдаётся только для разрешённых типов, в изолированном режиме без скриптов (`Content-Security-Policy`), с поддержкой HTTP Range для перемотки видео и аудио

Локальная сеть

  * HTTP-сервер доступен по LAN через `0.0.0.0:8080`
  * LAN-адрес выводится при запуске сервера
  * Доступ с Windows, Android или другого устройства в той же сети через браузер

Основа безопасности

  * Базовая защита путей от directory traversal
  * Операции с файлами запрещают выход за пределы корневого каталога хранения
  * Пути вложений чата генерируются сервером и не позволяют напрямую обращаться к произвольным файлам
  * Файлы остаются на компьютере-хосте и не загружаются в стороннее облако
  * Аутентификация устройств и доверенные устройства (v0.4.0)

### Пока не реализовано

  * Автоматическая синхронизация файлов
  * Миниатюры и предпросмотр документов Office
  * Постоянный полнотекстовый индекс поиска
  * Инструменты резервного копирования и восстановления
  * Windows Service / автоматический запуск

### Технологический стек

  * Backend: Go
  * HTTP: стандартная библиотека Go (`net/http`)
  * База данных: SQLite (`modernc.org/sqlite`)
  * Хранилище файлов: локальная файловая система
  * Realtime: WebSocket (`github.com/gorilla/websocket`)
  * Frontend: React
  * Язык: TypeScript
  * Сборщик: Vite
  * Клиент: веб-браузер
  * Удалённый доступ: Tailscale

### Запуск проекта

Требуется:

  * Go `1.27.x`
  * Node.js + npm
  * Windows для использования готовых `.bat`-скриптов

Для текущей ветки релизов протестирован Go `1.27.1`. Во frontend используются React `19.3.0` и Vite `8.0.0`.

Из корневой папки проекта:

```powershell
.\run.bat
```

Откройте Personal Cloud на компьютере-хосте:

```text
http://localhost:8080
```

Приложение автоматически создаёт каталоги:

```text
data/cloud.db
data/chat-attachments/
storage/
```

Чтобы открыть Personal Cloud с другого устройства в той же локальной сети, используйте LAN-адрес, который выводится при запуске сервера, например:

```text
LAN: http://192.168.1.50:8080
```

### Подключение устройств (v0.4.0)

1. Откройте Personal Cloud на компьютере-хосте по адресу `http://localhost:8080` — он доверяется автоматически.
2. Перейдите в **Устройства → Добавить устройство**. На 5 минут появятся одноразовый код и ссылка.
3. На телефоне или другом ПК откройте LAN-ссылку (или LAN-адрес и введите код), задайте название и подключите устройство.
4. Подключённое устройство остаётся доверенным, пока вы не отзовёте доступ в разделе **Устройства**.

Если доверенного устройства нет, используйте код подключения из консоли сервера (действует 10 минут).

Переменная окружения `PC_TRUST_LOCALHOST=0` включает обязательное подключение даже на компьютере-хосте. Запросы через прокси (с заголовками `X-Forwarded-*`) никогда не считаются запросами хоста.

### Удалённый доступ через Tailscale (v0.5.0)

1. Установите и авторизуйте Tailscale на компьютере-хосте.
2. Установите и авторизуйте Tailscale на удалённом телефоне или ПК в том же tailnet.
3. Запустите Personal Cloud. В консоли появится URL `Tailscale:`, если обнаружен Tailscale IPv4-адрес.
4. Откройте этот адрес на удалённом устройстве. Доступ всё равно защищён pairing и аутентификацией устройств Personal Cloud.
5. В web-интерфейсе также отображаются найденные маршруты LAN и Tailscale — нужный маршрут можно выбрать прямо там.

Tailscale предоставляет удалённый сетевой путь, а Personal Cloud продолжает слушать `0.0.0.0:8080`. В v0.5.0 приложение не настраивает Tailscale Serve или Funnel автоматически.

### Режим разработки

Backend:

```powershell
cd server
go run ./cmd/server
```

Frontend в отдельном терминале:

```powershell
cd web
npm install
npm run dev -- --host
```

Vite запускает frontend, а запросы к API проксируются на Go-сервер. Проксированные запросы содержат заголовки `X-Forwarded-*`, поэтому в режиме разработки браузер не считается хостом: подключите его по коду из консоли сервера.

### Сборка

```powershell
cd web
npm install
npm run build
cd ..

cd server
go mod tidy
go build -o ..\personal-cloud.exe .\cmd\server
```

Или:

```powershell
.\build.bat
```

### HTTP API

Основные методы:

```text
GET    /api/health
GET    /api/info
GET    /api/files
POST   /api/files
GET    /api/files/download
DELETE /api/files
POST   /api/folders
```

Управление файлами в v0.2.0:

```text
PATCH  /api/files/rename
POST   /api/files/move
POST   /api/files/copy
GET    /api/search?q=<query>
```

Поиск и предпросмотр в v0.7.0:

```text
GET    /api/search?q=<query>&type=<type>&modified=<period>&sort=<key>&order=<asc|desc>&content=1&path=<folder>&limit=<n>
GET    /api/files/preview?path=<file>        изображение / видео / аудио / PDF (поддерживает Range)
GET    /api/files/preview/text?path=<file>   первые 512 КБ текстового файла в UTF-8 (JSON)
```

Параметры поиска: `type` = `all`, `folder`, `image`, `video`, `audio`, `document`, `text`, `code`, `archive`, `other`; `modified` = `any`, `day`, `week`, `month`, `year`; `sort` = `relevance`, `name`, `size`, `modified`, `type`. В ответе: `items`, `terms` (для подсветки), `total`, `truncated` и `partial`.

Пример переименования:

```json
{"path":"docs/report.pdf","name":"final-report.pdf"}
```

Пример перемещения/копирования:

```json
{"path":"docs/report.pdf","destination":"archive"}
```

Примеры поиска:

```text
/api/search?q=report
/api/search?q=annual%20report&type=document&sort=modified
/api/search?q=TODO&content=1&type=code
/api/search?q=%22exact%20phrase%22%20ext:txt
```

Методы управления облаком в v0.6.0 (требуют доверенного устройства):

```text
GET    /api/storage                 метрики хранилища и диска хоста
GET    /api/settings                сохранённые настройки облака
PATCH  /api/settings                изменить cloudName, maxUploadMB, logRetentionDays
GET    /api/logs?limit=<n>&before=<id>&level=<level>&q=<query>
DELETE /api/logs                    очистить журнал событий
```

### Структура проекта

```text
personal-cloud/
├── server/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── api/
│   │   ├── auth/
│   │   ├── chat/
│   │   ├── config/
│   │   ├── database/
│   │   ├── files/
│   │   ├── network/
│   │   └── storage/
│   └── go.mod
├── web/
│   ├── src/main.tsx
│   ├── src/styles.css
│   ├── index.html
│   └── package.json
├── data/
│   ├── cloud.db
│   └── chat-attachments/
├── storage/
├── docs/
├── README.md
├── RELEASE_NOTES_v0.2.0.md
├── RELEASE_NOTES_v0.3.0.md
├── RELEASE_NOTES_v0.4.0.md
├── RELEASE_NOTES_v0.5.0.md
├── RELEASE_NOTES_v0.6.0.md
├── RELEASE_NOTES_v0.7.0.md
├── VERSION
├── build.bat
└── run.bat
```

### Roadmap

Проект развивается по схеме локальное хранилище → файловый менеджер → чат → защищённые устройства → удалённый доступ → управление облаком → предпросмотр/поиск → backup.

Версия | Этап
--- | ---
`v0.1.0` ✅ | Local Cloud — Go-сервер, SQLite, файловое хранилище, web-интерфейс, LAN, upload/download
`v0.2.0` ✅ | File Manager — переименование, перемещение, копирование, поиск, расширенные метаданные, потоковая передача и прогресс
`v0.3.0` ✅ | Personal Chat — сообщения, история, WebSocket, realtime, ссылки, изображения и вложения
`v0.4.0` ✅ | Secure Devices — идентификация устройств, pairing, авторизация, trusted devices и сессии
`v0.5.0` ✅ | Remote Cloud — Tailscale, удалённый доступ и выбор маршрута LAN / Tailscale
`v0.6.0` ✅ | Cloud Management — управление устройствами, метрики хранилища, настройки, журнал событий и конфигурация
`v0.7.0` ✅ | Preview & Search — расширенный поиск, поиск внутри текстовых файлов и предпросмотр файлов
`v0.8.0` | Backup & Recovery — backup, восстановление, проверки целостности и recovery-инструменты
`v1.0.0` | Personal Cloud — завершённая основная версия продукта

Полный подробный план разработки: `docs/project-plan.md`.

### Участие в разработке

Issues и pull requests приветствуются. Изменения должны соответствовать self-hosted архитектуре и плану проекта в `docs/`.

### Лицензия

MIT — проект можно свободно использовать и изменять с сохранением уведомления о лицензии.
