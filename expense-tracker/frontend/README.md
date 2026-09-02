# Expense Tracker — Frontend

Vite + React + TypeScript SPA for uploading a receipt. It requests a V4 signed
PUT URL, then uploads the file bytes straight to GCS.

## Deployment model (option B: same-origin, no CORS)

In production this SPA is packaged as an **nginx container on Cloud Run**. The
single container does two jobs:

1. Serves the built static SPA (`dist/`).
2. Reverse-proxies `/api/*` to the **CreateUploadURL** Cloud Run service.

Because the browser talks only to its own origin for the upload-URL request,
there is no cross-origin call and no CORS preflight to configure.

```
                          nginx container (Cloud Run)
  ┌────────┐  GET /            ┌──────────────────────────┐
  │        │ ────────────────► │  /usr/share/nginx/html    │  built SPA
  │        │                   │  (index.html + assets)    │
  │ Browser│  POST /api/upload-url  ┌────────────────────┐ │
  │        │ ─────────────────────► │ location /api/     │ │
  │        │                        │ proxy_pass         │ │
  └────────┘                        │   ${BACKEND_URL}/  │ │──► CreateUploadURL
      │                             └────────────────────┘ │      (Cloud Run)
      │                        └──────────────────────────┘        returns
      │  PUT <signed uploadUrl>  (absolute, straight to GCS)     { uploadUrl, ... }
      └──────────────────────────────────────────────────────►  GCS bucket
```

### Path agreement (api.ts ↔ nginx)

- The SPA POSTs to **`/api/upload-url`** (see `src/api.ts`). The base is
  `import.meta.env.VITE_API_BASE`, defaulting to the relative **`/api`**.
- nginx matches `location /api/` and proxies with a **trailing-slash**
  `proxy_pass ${BACKEND_URL}/;`, which strips the `/api/` prefix. So
  `POST /api/upload-url` reaches the backend as **`POST /upload-url`**.
- The CreateUploadURL functions-framework handler is a catch-all (it dispatches
  every path to the function and only checks the HTTP method), so it accepts
  `/upload-url`.

The direct `PUT` to the signed `uploadUrl` is an **absolute** URL and goes
straight to GCS — it is not proxied and is unchanged by this model.

## Runtime environment variables (container)

| Var           | Required | Default | Purpose                                                              |
| ------------- | -------- | ------- | ------------------------------------------------------------------- |
| `PORT`        | no       | `8080`  | Port nginx listens on. Cloud Run sets this automatically.           |
| `BACKEND_URL` | **yes**  | —       | Base URL of the CreateUploadURL service, e.g. `https://create-upload-url-xxxx.a.run.app`. Injected into the nginx config at container start. |

Both are substituted into `nginx/default.conf.template` by `envsubst` when the
container starts (the `nginx:alpine` entrypoint templates
`/etc/nginx/templates/*.template`). `BACKEND_URL` has no default — if it is
unset the generated `proxy_pass` is invalid and nginx will refuse to start, so
always provide it at deploy time.

## Local development

Build-time env vars use the `VITE_` prefix (see `.env.example`):

- `VITE_API_BASE` — base for the upload-URL request. Leave as `/api` for the
  proxied production model; for local dev against a backend on another origin,
  set it to that origin, e.g. `VITE_API_BASE=http://localhost:8080` (the request
  then goes to `http://localhost:8080/upload-url`).

```bash
npm install
npm run dev          # Vite dev server
npm test             # vitest
npm run build        # tsc -b && vite build -> dist/
```

## Building the container image

Docker is not required locally; the image is built in CI on **amd64**.

```bash
docker build -t expense-tracker-frontend .
```

The multi-stage `Dockerfile`:

- **Stage 1** (`node:22-alpine`): `npm ci` then `npm run build` → `dist/`.
- **Stage 2** (`nginx:1.27-alpine`): copies `dist/` to
  `/usr/share/nginx/html` and `nginx/default.conf.template` to
  `/etc/nginx/templates/default.conf.template`. Listens on `${PORT}` and
  reverse-proxies `/api/*` to `${BACKEND_URL}`.

Run it (providing the required env):

```bash
docker run -p 8080:8080 \
  -e BACKEND_URL=https://create-upload-url-xxxx.a.run.app \
  expense-tracker-frontend
```
