# Personal Cloud v0.1.0

Self-hosted personal cloud for your own devices.

v0.1.0 is the first working foundation: a Go server stores files on the host PC, keeps metadata in SQLite, and exposes a responsive web interface that can be opened from Windows, Android, or another device on the same LAN.

## Included in v0.1.0

- Go backend
- SQLite metadata database
- local filesystem storage
- upload files
- download files
- create folders
- delete files and folders
- basic file metadata
- responsive web UI for desktop and mobile
- mobile navigation drawer instead of the desktop sidebar
- mobile-friendly upload controls and file list
- LAN access on `0.0.0.0:8080`
- drag-and-drop upload
- security against basic path traversal

Not included yet: chat, authentication/device pairing, Tailscale integration, automatic sync, preview system, advanced search, backups, and Windows Service integration.

## Requirements

- Go 1.27.x
- Node.js + npm

Go 1.27.1 is the tested target for this release line. The frontend targets React 19.3 and Vite 8.

## Run on Windows

The first launch requires Node.js/npm to build the frontend and Go to run the backend. After the frontend has been built once, `run.bat` can be used for subsequent launches.

1. Open a terminal in the project root.
2. Build the frontend:

```powershell
cd web
npm install
npm run build
cd ..
```

3. Start the server using the included script:

```powershell
.\run.bat
```

4. On the PC open:

```text
http://localhost:8080
```

The server creates these directories automatically:

```text
data/cloud.db
storage/
```

## Open from an Android phone on the same Wi-Fi

Start the server and look at its console output. It will print a LAN URL similar to:

```text
LAN: http://192.168.1.50:8080
```

Open that address on the phone in a browser.

If Windows Firewall blocks the connection, allow the Go server through the private-network firewall prompt.

## Development mode

Run the backend:

```powershell
cd server
go run ./cmd/server
```

In another terminal run Vite:

```powershell
cd web
npm install
npm run dev -- --host
```

Vite will provide a development address. API requests are proxied to `localhost:8080`.

## Build a Windows executable

From the project root:

```powershell
cd server
go build -o personal-cloud.exe ./cmd/server
```

After building, keep `data/`, `storage/`, and `web/dist/` next to the executable/project structure. The easiest first test is to run the server from the repository root with `go run`.

## Project structure

```text
personal-cloud/
├── server/
│   ├── cmd/server/main.go
│   ├── internal/api/
│   ├── internal/config/
│   ├── internal/database/
│   ├── internal/files/
│   ├── internal/storage/
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
└── run.bat
```

## Troubleshooting

### PowerShell: `run.bat` is not recognized

PowerShell does not execute files from the current directory by bare name. Use:

```powershell
.\run.bat
```

### Go reports a missing `go.sum` entry

`run.bat` automatically runs `go mod download` and `go mod tidy`, so the first launch may take a little longer while Go downloads the SQLite dependency tree.

If you are running the server manually, use:

```powershell
cd server
go mod tidy
go run .\cmd\server
```
