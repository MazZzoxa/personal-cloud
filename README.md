# ☁️ Personal Cloud

A self-hosted personal cloud for your own devices, built with Go, React and TypeScript.

Current version: `v0.1.0`
Status: `v0.1.0` establishes the first working local cloud foundation with file storage, a responsive web interface and LAN access from other devices.
Author: @MazZzoxa

🇬🇧 English · 🇷🇺 Русский

## ☁️ Personal Cloud

### About

Personal Cloud is a self-hosted file storage system designed to run on your own home PC.

The main principle is simple: your files stay on your own computer, while your other devices can access them through a web browser on the same local network.

The project is intentionally designed for one owner and their own devices. More advanced features such as chat, remote access, authentication and synchronization are planned for later releases.

### Features — v0.1.0

File storage

  * Local filesystem storage on the host PC
  * SQLite database for file metadata
  * File and folder listing
  * Upload files
  * Download files
  * Create folders
  * Delete files and folders
  * Basic file metadata including size and modification time

Web interface

  * Responsive React web UI
  * Desktop and mobile layouts
  * Mobile navigation drawer
  * Mobile-friendly file list and upload controls
  * Drag-and-drop uploads

Local networking

  * HTTP server available on the LAN through `0.0.0.0:8080`
  * LAN address is printed when the server starts
  * Access from Windows, Android or another device with a browser on the same network

Security foundation

  * Basic file path validation against directory traversal
  * Files remain on the host machine instead of being uploaded to a third-party cloud

### Not included yet

  * Personal chat
  * WebSocket realtime messaging
  * Authentication and device pairing
  * Trusted device management
  * Tailscale remote access
  * Automatic file synchronization
  * File preview
  * Advanced search
  * Backup and recovery tools
  * PWA installation flow
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

Go `1.27.1` is the tested target for this release line. The frontend uses React `19.3.0` and Vite `8.0.0`.

Run the project from the repository root.

```powershell
.\run.bat
```

On the first launch, the script builds the frontend and prepares Go dependencies. After that the server starts from the repository root.

Open Personal Cloud on the host PC:

```text
http://localhost:8080
```

The server creates the application data automatically:

```text
data/cloud.db
storage/
```

### Open from another device on the same LAN

Start the server and check its console output. It prints a LAN address similar to:

```text
LAN: http://192.168.1.50:8080
```

Open that address in a browser on a phone, laptop or another device connected to the same network.

If Windows Firewall blocks the connection, allow the Go server through the private-network firewall prompt.

### Development mode

Run the backend:

```powershell
cd server
go run ./cmd/server
```

In another terminal run the Vite development server:

```powershell
cd web
npm install
npm run dev -- --host
```

Vite serves the frontend in development mode while API requests are proxied to the Go server.

### Building

Build the frontend:

```powershell
cd web
npm install
npm run build
cd ..
```

Build the Windows server executable:

```powershell
cd server
go mod tidy
go build -o ..\personal-cloud.exe .\cmd\server
```

Or use the included build script from the repository root:

```powershell
.\build.bat
```

The executable must be used together with the built frontend in `web/dist/` and the application data directories.

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
│   ├── src/
│   ├── public/
│   ├── index.html
│   ├── package.json
│   └── vite.config.ts
├── data/
├── storage/
├── docs/
├── README.md
├── RELEASE_NOTES_v0.1.0.md
├── VERSION
├── build.bat
└── run.bat
```

### HTTP API

The first release exposes a small HTTP API for the core storage workflow:

```text
GET    /api/health
GET    /api/info
GET    /api/files
POST   /api/files
GET    /api/files/download
DELETE /api/files
POST   /api/folders
```

### Roadmap

The project follows a gradual local-cloud progression.

Version  | Milestone
--- | ---
v0.1.0 ✅  | Local Cloud: Go server, SQLite foundation, filesystem storage, web UI, LAN access, upload and download
v0.2.0  | File Manager: folders, rename, move, copy, search, metadata, streaming and progress
v0.3.0  | Personal Chat: messages, history, WebSocket, realtime, links and file attachments
v0.4.0  | Secure Devices: device identity, pairing, authorization, trusted devices and sessions
v0.5.0  | Remote Cloud: Tailscale, Internet access and LAN / Internet handling
v0.6.0  | PWA: installable mobile web app, responsive navigation and supported notifications
v0.7.0  | Cloud Management: devices, storage information, settings, logs and configuration
v0.8.0  | Preview & Search: advanced search and file preview
v0.9.0  | Backup & Recovery: backup, restore, integrity checks and recovery tools
v1.0.0  | Personal Cloud: complete core product

Full project plan: `docs/project-plan.md`.

### Contributing

Issues and pull requests are welcome. Please keep changes consistent with the self-hosted, single-owner architecture and the project plan in `docs/project-plan.md`.

### License

MIT — use and modify the project freely while keeping the license notice.

## 🇷🇺 Русский

### О проекте

Personal Cloud — личное облачное хранилище, которое работает на собственном домашнем ПК и предоставляет доступ к файлам через веб-браузер.

Главный принцип проекта прост: файлы остаются на компьютере владельца, а другие собственные устройства могут получать к ним доступ через локальную сеть.

Проект изначально рассчитан на одного владельца и его устройства. Чат, удалённый доступ через Интернет, авторизация и синхронизация будут добавляться в следующих версиях.

### Возможности — v0.1.0

Файловое хранилище

  * Хранение файлов в файловой системе ПК
  * SQLite для метаданных файлов
  * Список файлов и папок
  * Загрузка файлов
  * Скачивание файлов
  * Создание папок
  * Удаление файлов и папок
  * Базовые метаданные, включая размер и дату изменения

Веб-интерфейс

  * Адаптивный React-интерфейс
  * Отдельные desktop и mobile представления
  * Мобильная панель навигации
  * Адаптированные для телефона список файлов и управление загрузкой
  * Drag-and-drop загрузка

Локальная сеть

  * HTTP-сервер доступен по LAN через `0.0.0.0:8080`
  * При запуске сервер выводит LAN-адрес
  * Доступ с Windows, Android или другого устройства через браузер в той же сети

Основа безопасности

  * Базовая проверка файловых путей против directory traversal
  * Файлы остаются на компьютере владельца и не передаются стороннему облачному сервису

### Пока не реализовано

  * Личный чат
  * Realtime-сообщения через WebSocket
  * Аутентификация и pairing устройств
  * Управление доверенными устройствами
  * Удалённый доступ через Tailscale
  * Автоматическая синхронизация файлов
  * Предпросмотр файлов
  * Расширенный поиск
  * Backup и восстановление
  * Полноценная установка как PWA
  * Windows Service / автоматический запуск

### Технологический стек

  * Backend: Go
  * HTTP: стандартная библиотека Go (`net/http`)
  * База данных: SQLite (`modernc.org/sqlite`)
  * Хранилище файлов: файловая система
  * Frontend: React
  * Язык: TypeScript
  * Сборщик: Vite
  * Клиент: веб-браузер
  * Удалённый доступ: интеграция Tailscale запланирована

### Запуск проекта

Требуется:

  * Go `1.27.x`
  * Node.js + npm
  * Windows для предоставленных `.bat`-скриптов

Для этой версии тестовой целью является Go `1.27.1`. Frontend использует React `19.3.0` и Vite `8.0.0`.

Запускайте проект из корня репозитория:

```powershell
.\run.bat
```

При первом запуске скрипт собирает frontend и подготавливает зависимости Go. После этого запускается Go-сервер.

На компьютере с сервером откройте:

```text
http://localhost:8080
```

Приложение автоматически создаёт:

```text
data/cloud.db
storage/
```

### Подключение с другого устройства по LAN

Запустите сервер и посмотрите вывод консоли. Он покажет адрес вида:

```text
LAN: http://192.168.1.50:8080
```

Откройте этот адрес в браузере на телефоне, ноутбуке или другом устройстве, подключённом к той же сети.

Если Windows Firewall блокирует соединение, разрешите Go-серверу работать через частную сеть.

### Режим разработки

Запустите backend:

```powershell
cd server
go run ./cmd/server
```

В другом терминале запустите Vite:

```powershell
cd web
npm install
npm run dev -- --host
```

В режиме разработки frontend обслуживается Vite, а API-запросы проксируются на Go-сервер.

### Сборка

Соберите frontend:

```powershell
cd web
npm install
npm run build
cd ..
```

Соберите Windows-исполняемый файл:

```powershell
cd server
go mod tidy
go build -o ..\personal-cloud.exe .\cmd\server
```

Или используйте готовый скрипт из корня проекта:

```powershell
.\build.bat
```

Исполняемый файл должен использоваться вместе со собранным frontend в `web/dist/` и каталогами данных приложения.

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
│   ├── src/
│   ├── public/
│   ├── index.html
│   ├── package.json
│   └── vite.config.ts
├── data/
├── storage/
├── docs/
├── README.md
├── RELEASE_NOTES_v0.1.0.md
├── VERSION
├── build.bat
└── run.bat
```

### HTTP API

В первой версии доступен небольшой HTTP API для основной работы с хранилищем:

```text
GET    /api/health
GET    /api/info
GET    /api/files
POST   /api/files
GET    /api/files/download
DELETE /api/files
POST   /api/folders
```

### Roadmap

Проект развивается поэтапно от локального файлового облака к полноценному Personal Cloud.

Версия  | Этап
--- | ---
v0.1.0 ✅  | Local Cloud: Go-сервер, SQLite foundation, файловое хранилище, web UI, LAN, загрузка и скачивание
v0.2.0  | File Manager: папки, rename, move, copy, поиск, метаданные, streaming и progress
v0.3.0  | Personal Chat: сообщения, история, WebSocket, realtime, ссылки и вложения
v0.4.0  | Secure Devices: идентификация устройств, pairing, авторизация, trusted devices и сессии
v0.5.0  | Remote Cloud: Tailscale, доступ через Интернет и автоматическая работа LAN / Internet
v0.6.0  | PWA: устанавливаемое мобильное веб-приложение и адаптивная навигация
v0.7.0  | Cloud Management: устройства, информация о хранилище, настройки, логи и конфигурация
v0.8.0  | Preview & Search: расширенный поиск и просмотр файлов
v0.9.0  | Backup & Recovery: backup, restore, проверки целостности и инструменты восстановления
v1.0.0  | Personal Cloud: полноценный основной продукт

Полный план проекта: `docs/project-plan.md`.

### Участие в разработке

Issues и pull requests приветствуются. Изменения желательно сохранять в рамках self-hosted архитектуры для одного владельца и текущего плана проекта.

### Лицензия

MIT — проект можно свободно использовать и изменять с сохранением уведомления о лицензии.
