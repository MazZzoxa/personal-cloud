# Personal Cloud v0.1.0 — Local Cloud

First development release of Personal Cloud. This version establishes the self-hosted storage foundation: a Go server runs on the home PC, files stay on the local filesystem, metadata is stored in SQLite, and the cloud can be opened from a browser on the PC or another device on the LAN.

## Features

- Local self-hosted file storage
- SQLite metadata database
- File and folder listing
- Upload files
- Download files
- Create folders
- Delete files and folders
- Responsive web interface for desktop and mobile
- Mobile navigation drawer and phone-sized file layout
- Mobile upload controls optimized for touch screens
- LAN access on port `8080`
- Drag-and-drop uploads
- Basic path validation against directory traversal

## Not included yet

- Personal chat
- WebSocket realtime messaging
- Authentication and device pairing
- Tailscale remote access
- Automatic file synchronization
- File preview
- Advanced search
- Backup and recovery tools
- Windows Service / automatic startup

## Next release direction

v0.2.0 will focus on a more complete file manager: rename, move, copy, better metadata, search, streaming/progress improvements, and a more polished mobile experience.
