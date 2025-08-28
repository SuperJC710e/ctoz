<!--
 * @Author: a624669980@163.com a624669980@163.com
 * @Date: 2025-08-21 11:15:27
 * @LastEditors: a624669980@163.com a624669980@163.com
 * @LastEditTime: 2025-08-28 17:26:26
 * @FilePath: /CtoZ/README.md
 * @Description: 这是默认设置,请设置`customMade`, 打开koroFileHeader查看配置 进行设置: https://github.com/OBKoro1/koro1FileHeader/wiki/%E9%85%8D%E7%BD%AE
-->
# CTOZ - CasaOS to ZimaOS Migration Tool

A professional migration tool to move from CasaOS to ZimaOS. It supports both online and offline migration modes and helps complete full application migrations.

## Features

CTOZ provides Online Migration (both CasaOS and ZimaOS are online) and Offline Migration (only one is online). It downloads data from the source system and imports it into the target system to assist in completing full application migration.

## Use Case

If you have a CasaOS device and find ZimaOS more suitable, you may want to switch. This tool helps you migrate applications and data from CasaOS to ZimaOS with minimal friction.

## Migration Scope

This tool migrates everything under the application configuration (AppData) directory and the application YAML/Compose files. Applications will be re-installed on ZimaOS.

- All contents under AppData (user data and configurations)
- Application definitions (Docker Compose / YAML)

## Quick Start

### Docker Compose
```bash
# Clone repository
git clone https://github.com/LinkLeong/ctoz.git
cd ctoz

# Start services
docker-compose up -d
```

### Docker CLI
```bash
# Run container
docker run --rm -p 8080:8080 a624669980/ctoz:latest
```

## Technical Highlights

- Online Migration: Direct connection between source and target, real-time transfer
- Offline Migration: Export a package first, then import to the target
- Live Monitoring: Real-time status updates and logs via WebSocket
- Smart Caching: Import status query caching for faster responses
- Web UI: Modern, easy-to-use web interface

## Notes

### When part of the migration fails
- AppData upload always succeeds. If a folder already exists on ZimaOS, a numeric suffix will be appended.
- For Docker installation failures, download the YAML and import manually on ZimaOS.

### Import status not showing
- Import status aggregates all apps and may take time. Please wait.
- Query performance is optimized. Repeat queries will use cache for faster response.

## Development

### Backend (Go)
```bash
cd backend
go mod tidy
go run cmd/main.go
```

### Frontend (React + TypeScript)
```bash
cd frontend
npm install
npm run dev
```

## API Docs

After starting the service, open: http://localhost:8080/info

## Contributing

Issues and PRs are welcome!

## Support us

Hey, do you like this kind of project?

To make sure we can keep working on free and open-source projects like this,
please consider becoming a ❤️ Sponsor or support us via ☕ Ko-fi.

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/V7V71KA9CA)

## License

MIT License