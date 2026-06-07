# kube-deploy — manual step-by-step commands

All commands below are run manually (no helper scripts). Use **PowerShell** or **Git Bash** from the repo root unless noted.

```text
C:\Users\Asus\Desktop\kube-deploy
```

---

## Prerequisites

| Tool | Purpose |
|------|---------|
| [Docker Desktop](https://docs.docker.com/desktop/setup/install/windows-install/) | Runs kind nodes |
| [Go 1.22+](https://go.dev/dl/) | Build the API |
| `kubectl` | Talk to the cluster (ships with Docker Desktop) |
| `kind` | Local Kubernetes cluster |

---

## Step 1 — Install kind (Windows, one time)

### 1.1 Download kind

**PowerShell:**

```powershell
New-Item -ItemType Directory -Force -Path "$env:LOCALAPPDATA\Programs\kind"
Invoke-WebRequest -Uri "https://github.com/kubernetes-sigs/kind/releases/download/v0.27.0/kind-windows-amd64" `
  -OutFile "$env:LOCALAPPDATA\Programs\kind\kind.exe" -UseBasicParsing
```

### 1.2 Add kind to PATH (permanent)

Add this **folder** to your user **Path** (not the `.exe` file):

```text
C:\Users\Asus\AppData\Local\Programs\kind
```

**GUI:** Win + R → `sysdm.cpl` → Advanced → Environment Variables → User **Path** → New → paste the folder above → OK.

**Fully quit and reopen Cursor** (or sign out of Windows) so new terminals see PATH.

### 1.3 Verify

```powershell
kind version
```

If not found in an old terminal:

```powershell
& "$env:LOCALAPPDATA\Programs\kind\kind.exe" version
```

**Alternative:** `winget install Kubernetes.kind` then restart Cursor.

---

## Step 2 — Start Docker

1. Open **Docker Desktop**.
2. Wait until it shows **Running**.

```powershell
docker info
```

---

## Step 3 — Create the kind cluster (each new cluster)

```powershell
cd C:\Users\Asus\Desktop\kube-deploy

kind create cluster --name kube-deploy --config scripts/kind-config.yaml

kind export kubeconfig --name kube-deploy

kubectl cluster-info --context kind-kube-deploy

kubectl get nodes
```

---

## Step 4 — Install ingress-nginx (once per cluster)

```powershell
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml

kubectl wait --namespace ingress-nginx deployment/ingress-nginx-controller --for=condition=Available --timeout=300s
```

Check:

```powershell
kubectl get pods -n ingress-nginx
```

---

## Step 5 — Install metrics-server (once per cluster)

```powershell
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
```

Patch for kind (PowerShell — avoids quoting issues):

```powershell
$patch = '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
$f = "$env:TEMP\metrics-server-patch.json"
[System.IO.File]::WriteAllText($f, $patch)
kubectl patch -n kube-system deployment metrics-server --type=json --patch-file $f
```

**Git Bash** (same patch):

```bash
kubectl patch -n kube-system deployment metrics-server --type=json -p='[
  {"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}
]'
```

Verify (optional):

```powershell
kubectl top nodes
```

---

## Step 6 — Build and run the API

```powershell
cd C:\Users\Asus\Desktop\kube-deploy

go mod tidy

go build -o bin\kube-deploy.exe .

# Optional but recommended: require Authorization: Bearer dev-token
$env:API_TOKEN = "dev-token"

# Optional: default host is 127.0.0.1; non-loopback HOST requires API_TOKEN
# $env:HOST = "0.0.0.0"

# Optional: default rollout wait is 2 minutes
# $env:ROLLOUT_TIMEOUT = "2m"

.\bin\kube-deploy.exe
```

Leave this terminal open. You should see:

```text
kube-deploy API listening on http://localhost:8080
```

**Notes:**

- Prefer `go build` + `.\bin\kube-deploy.exe` over `go run .` on Windows if Application Control blocks temp executables.
- Different port: `$env:PORT = "8081"` then run the exe again.
- If port 8080 is in use, stop the old process or change `PORT`.

---

## Step 7 — Health check (new terminal)

**Git Bash / curl:**

```bash
curl http://localhost:8080/healthz
```

**PowerShell:**

```powershell
Invoke-RestMethod http://localhost:8080/healthz
```

---

## Step 8 — Deploy an application

**Git Bash** (one line):

```bash
cd /c/Users/Asus/Desktop/kube-deploy

curl -X POST http://localhost:8080/deploy \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer dev-token" \
  -d "@examples/deploy-request.json"
```

**PowerShell:**

```powershell
cd C:\Users\Asus\Desktop\kube-deploy

Invoke-RestMethod -Method POST -Uri http://localhost:8080/deploy `
  -ContentType "application/json" `
  -Headers @{ Authorization = "Bearer dev-token" } `
  -InFile "examples\deploy-request.json"
```

**Windows curl.exe:**

```powershell
curl.exe -X POST http://localhost:8080/deploy -H "Content-Type: application/json" -H "Authorization: Bearer dev-token" -d "@examples/deploy-request.json"
```

If you did not set `API_TOKEN`, omit the `Authorization` header.

Save the `id` and `ingressHost` from the JSON response.

---

## Step 9 — Query the API

```bash
curl http://localhost:8080/deployments

curl http://localhost:8080/deployments/<deployment-id>
```

PowerShell:

```powershell
Invoke-RestMethod http://localhost:8080/deployments
Invoke-RestMethod http://localhost:8080/deployments/<deployment-id>
```

---

## Step 10 — Verify in Kubernetes

```powershell
kubectl get all -n demo-api

kubectl get ingress,hpa,pdb,networkpolicy -n demo-api

kubectl get pods -n demo-api

kubectl describe pods -n demo-api
```

---

## Step 11 — Access the app

### Option A — Port forward (simplest)

```powershell
kubectl port-forward -n demo-api svc/demo-api 8080:8080
```

Open: http://localhost:8080

### Option B — Ingress (kind)

1. From the deploy response, note `ingressHost` (e.g. `demo-api.7d697f4e.local`).
2. Edit `C:\Windows\System32\drivers\etc\hosts` as Administrator and add:

```text
127.0.0.1 demo-api.<your-id-prefix>.local
```

3. Open: http://demo-api.<your-id-prefix>.local

---

## Step 12 — Stop everything

### Stop the API

In the terminal running `kube-deploy.exe`: **Ctrl+C**

Or find and kill the process on port 8080:

```powershell
Get-NetTCPConnection -LocalPort 8080 | Select-Object OwningProcess
Stop-Process -Id <PID> -Force
```

### Delete the kind cluster

```powershell
kind delete cluster --name kube-deploy

kind get clusters
```

### Stop Docker (optional)

Quit Docker Desktop from the system tray.

---

## Quick reference — full flow from scratch

```powershell
# 1. Cluster
kind create cluster --name kube-deploy --config scripts/kind-config.yaml
kind export kubeconfig --name kube-deploy

# 2. Addons
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
kubectl wait --namespace ingress-nginx deployment/ingress-nginx-controller --for=condition=Available --timeout=300s
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
# ... metrics-server patch (Step 5)

# 3. API (separate terminal)
go build -o bin\kube-deploy.exe .
$env:API_TOKEN = "dev-token"
.\bin\kube-deploy.exe

# 4. Deploy (another terminal)
curl.exe -X POST http://localhost:8080/deploy -H "Content-Type: application/json" -H "Authorization: Bearer dev-token" -d "@examples/deploy-request.json"

# 5. Verify
kubectl get all -n demo-api
```

---

## Troubleshooting

| Problem | What to do |
|---------|------------|
| `kind` not recognized | PATH must be folder `...\Programs\kind`, not `kind.exe`. Restart Cursor. Or use full path to `kind.exe`. |
| Docker API connection failed | Start Docker Desktop and wait until Running. |
| Port 8080 in use | Stop old `kube-deploy.exe` or set `$env:PORT = "8081"`. |
| `go run` blocked by Windows | Use `go build -o bin\kube-deploy.exe .` instead. |
| PowerShell `curl` fails | Use `curl.exe` or `Invoke-RestMethod`. |
| Multi-line curl in Bash failed | Use one line, or end lines with `\` (not PowerShell `` ` ``). |
| Pods not Running | `kubectl describe pods -n demo-api` — image must work non-root (e.g. nginx-unprivileged). |
| API returns 401 | `API_TOKEN` is set; include `Authorization: Bearer <token>`. |
| API deploy 502 | API needs valid kubeconfig; cluster must exist (`kind export kubeconfig`). |
| VIBSL deploy exits on startup | Set `API_TOKEN` in VIBSL env vars; set health check path to `/health`. Provide kubeconfig for `/deploy`. |

---

## Deploy on VIBSL

[VIBSL](https://vibsl.com) auto-detects Go, builds a container, and probes **`GET /health`** on port **8080**. This repo exposes `/health` (and `/healthz`) for that check.

Set these **environment variables** in the VIBSL dashboard (do not commit secrets to git — VIBSL scans for leaked credentials):

| Variable | Required | Example |
|----------|----------|---------|
| `API_TOKEN` | **Yes** | `my-secure-random-token` |
| `KUBECONFIG_PATH` | For `/deploy` | Path to mounted kubeconfig in the container |
| `HOST` | No | Auto `0.0.0.0` when VIBSL sets `PORT` |
| `PORT` | No | Set automatically by VIBSL (default `8080`) |

The API **starts without a Kubernetes cluster** (health check passes). `POST /deploy` returns **502** until valid cluster credentials are configured (`KUBECONFIG_PATH`, `KUBECONFIG`, or in-cluster config when running inside Kubernetes).

Example request after deploy:

```bash
curl -X POST https://your-app.vibsl.app/deploy \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer my-secure-random-token" \
  -d "@examples/deploy-request.json"
```

---

## API endpoints

| Method | Path |
|--------|------|
| `GET` | `/healthz` |
| `GET` | `/health` (alias for PaaS health checks) |
| `POST` | `/deploy` |
| `GET` | `/deployments` |
| `GET` | `/deployments/{id}` |

Example deploy body: `examples/deploy-request.json`
