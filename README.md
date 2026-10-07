# ☁️ Personal Cloud

A self-hosted personal cloud for your own devices, built with Go, React and TypeScript — a simple local file manager for accessing your files from a browser over your own network.

Current version: `v0.2.0`  
Status: `v0.2.0` turns the initial local storage foundation into a more complete file manager with rename, move, copy, search, extended metadata and improved file transfers.  
Author: @MazZzoxa

🇬🇧 English · 🇷🇺 Русский

* * *

## ☁️ Personal Cloud

### About

Personal Cloud is a self-hosted personal cloud and file manager designed to run on your own home PC.

The main principle is simple: your files stay on your own computer, while your own devices can access them through a web browser over the local network. The project is intentionally designed around personal use and your own devices rather than a public multi-user cloud service.

### Features — v0.2.0

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

Local networking

  * HTTP server available on the LAN through `0.0.0.0:8080`
  * LAN address printed when the server starts
  * Access from Windows, Android or another browser on the same network

Security foundation

  * Basic file path validation against directory traversal
  * File operations reject destinations outside the storage root
  * Files remain on the host machine instead of being uploaded to a third-party cloud

### Not included yet

  * Personal chat
  * WebSocket realtime messaging
  * Authentication and device pairing
  * Trusted device management
  * Tailscale remote access
  * Automatic file synchronization
  * File preview
  * Advanced search filters and indexing
  * Backup and recovery tools
  * Full PWA installation flow
  * Windows Service / automatic startup

### Tech stack

  * Backend: Go
  * HTTP: Go standard library (`net/http`)
  * Database: SQLite (`modernc.org/sqlite`)
  * File storage: local filesystem
  * Frontend: React
  * Language: TypeScript
  * Build tool: Vite
  * Client: web browser
  * Remote networking: planned Tailscale integration

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
storage/
```

To access it from another device on the same LAN, use the LAN URL printed by the server, for example:

```text
LAN: http://192.168.1.50:8080
```

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

Vite serves the frontend while API requests are proxied to the Go server.

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

Rename payload:

```json
{"path":"docs/report.pdf","name":"final-report.pdf"}
```

Move/copy payload:

```json
{"path":"docs/report.pdf","destination":"archive"}
```

Search example:

```text
/api/search?q=report
```

### Project structure

```text
personal-cloud/
├── server/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── api/
│   │   ├── config/
│   │   ├── database/
│   │   ├── files/
│   │   └── storage/
│   └── go.mod
├── web/
│   ├── src/main.tsx
│   ├── src/styles.css
│   ├── index.html
│   └── package.json
├── data/
├── storage/
├── docs/
├── README.md
├── RELEASE_NOTES_v0.2.0.md
├── VERSION
├── build.bat
└── run.bat
```

### Roadmap

The project follows a local storage → file manager → chat → secure devices → remote access → PWA → management → preview/search → backup progression.

Version | Milestone
--- | ---
`v0.1.0` ✅ | Local Cloud — Go server, SQLite foundation, filesystem storage, web UI, LAN, upload/download
`v0.2.0` ✅ | File Manager — rename, move, copy, search, extended metadata, streaming and progress
`v0.3.0` | Personal Chat — messages, history, WebSocket, realtime, links and file attachments
`v0.4.0` | Secure Devices — device identity, pairing, authorization, trusted devices and sessions
`v0.5.0` | Remote Cloud — Tailscale, Internet access and LAN / Internet handling
`v0.6.0` | PWA — installable mobile web app and mobile-first navigation
`v0.7.0` | Cloud Management — devices, storage information, settings, logs and configuration
`v0.8.0` | Preview & Search — advanced search and file preview
`v0.9.0` | Backup & Recovery — backup, restore, integrity checks and recovery tools
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

### Возможности — v0.2.0

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

Локальная сеть

  * HTTP-сервер доступен по LAN через `0.0.0.0:8080`
  * LAN-адрес выводится при запуске сервера
  * Доступ с Windows, Android или другого устройства в той же сети через браузер

Основа безопасности

  * Базовая защита путей от directory traversal
  * Операции с файлами запрещают выход за пределы корневого каталога хранения
  * Файлы остаются на компьютере-хосте и не загружаются в стороннее облако

### Пока не реализовано

  * Личный чат
  * WebSocket и обмен сообщениями в реальном времени
  * Аутентификация и привязка устройств
  * Управление доверенными устройствами
  * Удалённый доступ через Tailscale
  * Автоматическая синхронизация файлов
  * Предпросмотр файлов
  * Расширенные фильтры и индексация поиска
  * Инструменты резервного копирования и восстановления
  * Полноценный PWA-режим установки
  * Windows Service / автоматический запуск

### Технологический стек

  * Backend: Go
  * HTTP: стандартная библиотека Go (`net/http`)
  * База данных: SQLite (`modernc.org/sqlite`)
  * Хранилище файлов: локальная файловая система
  * Frontend: React
  * Язык: TypeScript
  * Сборщик: Vite
  * Клиент: веб-браузер
  * Удалённый доступ: планируется интеграция с Tailscale

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
storage/
```

Чтобы открыть Personal Cloud с другого устройства в той же локальной сети, используйте LAN-адрес, который выводится при запуске сервера, например:

```text
LAN: http://192.168.1.50:8080
```

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

Vite запускает frontend, а запросы к API проксируются на Go-сервер.

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

Пример переименования:

```json
{"path":"docs/report.pdf","name":"final-report.pdf"}
```

Пример перемещения/копирования:

```json
{"path":"docs/report.pdf","destination":"archive"}
```

Пример поиска:

```text
/api/search?q=report
```

### Структура проекта

```text
personal-cloud/
├── server/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── api/
│   │   ├── config/
│   │   ├── database/
│   │   ├── files/
│   │   └── storage/
│   └── go.mod
├── web/
│   ├── src/main.tsx
│   ├── src/styles.css
│   ├── index.html
│   └── package.json
├── data/
├── storage/
├── docs/
├── README.md
├── RELEASE_NOTES_v0.2.0.md
├── VERSION
├── build.bat
└── run.bat
```

### Roadmap

Проект развивается по схеме локальное хранилище → файловый менеджер → чат → защищённые устройства → удалённый доступ → PWA → управление → предпросмотр/поиск → backup.

Версия | Этап
--- | ---
`v0.1.0` ✅ | Local Cloud — Go-сервер, SQLite, файловое хранилище, web-интерфейс, LAN, upload/download
`v0.2.0` ✅ | File Manager — переименование, перемещение, копирование, поиск, расширенные метаданные, потоковая передача и прогресс
`v0.3.0` | Personal Chat — сообщения, история, WebSocket, realtime, ссылки и вложения
`v0.4.0` | Secure Devices — идентификация устройств, pairing, авторизация, trusted devices и сессии
`v0.5.0` | Remote Cloud — Tailscale, доступ через Интернет и выбор LAN / Internet-маршрута
`v0.6.0` | PWA — устанавливаемое мобильное web-приложение и mobile-first навигация
`v0.7.0` | Cloud Management — устройства, информация о хранилище, настройки, логи и конфигурация
`v0.8.0` | Preview & Search — расширенный поиск и предпросмотр файлов
`v0.9.0` | Backup & Recovery — backup, восстановление, проверки целостности и recovery-инструменты
`v1.0.0` | Personal Cloud — завершённая основная версия продукта

Полный подробный план разработки: `docs/project-plan.md`.

### Участие в разработке

Issues и pull requests приветствуются. Изменения должны соответствовать self-hosted архитектуре и плану проекта в `docs/`.

### Лицензия

MIT — проект можно свободно использовать и изменять с сохранением уведомления о лицензии.
