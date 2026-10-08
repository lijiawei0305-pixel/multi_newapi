# New API

Next-generation LLM gateway and AI asset management system. One deployment exposes a unified API across model providers, with user management, usage accounting, and an admin console.

Use it only for lawful, authorized gateway and private-deployment workloads. Obtain upstream API keys and model access lawfully, and follow the upstream terms and the laws that apply to you.

## Install

You need Docker with Docker Compose, or Go 1.26.8 (the version in `go.mod`) and [Bun](https://bun.sh) when you work on the web app.

```bash
git clone https://github.com/QuantumNous/new-api.git
cd new-api
cp .env.example .env
openssl rand -hex 32   # write a fresh value into POSTGRES_PASSWORD
openssl rand -hex 32   # write a different fresh value into REDIS_PASSWORD
```

`.env.example` leaves secrets empty on purpose. Fill `.env` locally and do not commit it.

## Run

`docker-compose.yml` is a local HTTP Quick Start. It listens on `http://localhost:3000` and explicitly opts into local development mode. Do not expose that mode on the public internet.

```bash
docker compose up -d
```

The same local mode as a single container, using SQLite under `./data`:

```bash
docker run --name new-api -d --restart always \
  -p 3000:3000 \
  -e DEPLOYMENT_ENV=development \
  -e SESSION_COOKIE_SECURE=false \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  calciumion/new-api:latest
```

## Configuration

| Variable | Description |
| --- | --- |
| `DEPLOYMENT_ENV` | Security mode. Unset or unknown values are treated as `production`. Local source or HTTP development must explicitly use `development`. |
| `SESSION_COOKIE_SECURE` | Send the session cookie only over HTTPS. Production requires `true`. Use `false` only together with explicit local `DEPLOYMENT_ENV=development`. |
| `SESSION_SECRET` | Session-signing secret. Required in production. Generate it with `openssl rand -hex 32`. |
| `CRYPTO_SECRET` | Encryption and HMAC secret. Required in production, and it must differ from `SESSION_SECRET`. |
| `SQL_DSN` | Database connection string when the process does not use the Compose-managed database. |
| `REDIS_CONN_STRING` | Redis connection string when the process does not use the Compose-managed Redis. |

Omitting `DEPLOYMENT_ENV` does not enable an insecure development fallback. A local source install served over HTTP must explicitly set `DEPLOYMENT_ENV=development`. Internet-facing deployments must stay in production mode, terminate HTTPS, set `SESSION_COOKIE_SECURE=true`, and provide two different randomly generated `SESSION_SECRET` and `CRYPTO_SECRET` values. Never commit those secrets.

The rest of the local template is in `.env.example`.

## Development and tests

From the repository root:

```bash
bash scripts/preflight.sh
bash deploy/ops/tests/run.sh
bash scripts/check-docker-context-secrets.sh
```

`scripts/preflight.sh` is the shared local, CI, and release gate. Set `PREFLIGHT_SCOPE` to `all` (the default), `backend`, `default`, `classic`, `orbit`, or `electron`. The Docker context check also runs inside the backend scope of that script.

Default frontend:

```bash
cd web/default
bun install --frozen-lockfile
bun run dev
```

## License

This project is licensed under the [GNU Affero General Public License v3.0 (AGPLv3)](./LICENSE). Additional terms under AGPLv3 Section 7 are in [NOTICE](./NOTICE).

Modified versions must preserve the author attribution notice `Frontend design and development by New API contributors.` in the appropriate legal notices and in any prominent about, legal, footer, or attribution location presented by the user interface.

Modified versions that present a user interface must also preserve a visible link to the original project: <https://github.com/QuantumNous/new-api>.

Third-party dependency notices are in [THIRD-PARTY-LICENSES.md](./THIRD-PARTY-LICENSES.md).

This program is developed from [One API](https://github.com/songquanpeng/one-api) (MIT License).

Copyright (c) QuantumNous and contributors.
