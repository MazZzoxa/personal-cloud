# Personal Cloud v0.4.0 — Secure Devices

## What's new

- **Device identity** — every client is a device with a name, last activity and last IP.
- **Pairing** — a new device connects once with a single-use 8-character code (valid 5 minutes) or a pairing link `/#pair=XXXX-XXXX`.
- **Authentication** — all API routes, downloads, chat attachments and the chat WebSocket require a trusted device. Only `GET /api/health`, `GET /api/auth/me` and `POST /api/auth/pair` are public.
- **Trusted devices** — the browser keeps a random token in an `HttpOnly`, `SameSite=Lax` cookie; the server stores only its SHA-256 hash. Devices stay trusted until revoked.
- **Revoke access** — the new *Devices* view lists devices (online status, last activity), renames them and revokes access. Revocation disconnects live WebSocket sessions at once and returns the device to the pairing screen.
- **Host PC** — direct `localhost` connections are trusted automatically as the built-in, non-revocable host device (`PC_TRUST_LOCALHOST=0` disables this). Requests with proxy headers are never treated as the host.
- **Pairing code in the console** — a fresh code (valid 10 minutes) is printed at server start.

## Hardening

- Per-IP (5) and global (30) failed-attempt limits for pairing within 10 minutes.
- Same-origin check for all state-changing requests (CSRF) and same-origin WebSocket upgrade.
- Pairing code travels in the URL fragment, so it is not sent to the server by the link itself.

## API

```text
GET    /api/auth/me
POST   /api/auth/pair
POST   /api/auth/logout
GET    /api/devices
POST   /api/devices/pairing
PATCH  /api/devices/{id}
DELETE /api/devices/{id}
```

## Database

New tables `devices` and `pairing_codes` are created automatically on startup. Existing data is untouched.

## Upgrade notes

- After upgrading, the host PC works as before. Phones and other PCs must be paired once.
- Plain HTTP on the LAN is still used; traffic is not encrypted until the Tailscale/HTTPS work in v0.5.0.
