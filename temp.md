# Walkthrough: Kubernetes Manifests & Deployment Setup

All Kubernetes manifests, Kustomize overlays, and Dockerfile adjustments have been created and validated for the Xsolla 5-service ecosystem.
The Kustomize base is the single source of truth for shared Kubernetes resources; environment-specific configuration belongs in an overlay.

---

## Changes Summary

### 1. Codebase Modifications
- [xsolla-capstone/notification-service/Dockerfile](file:///home/maqa/xsolla/xsolla-capstone/notification-service/Dockerfile): Added `COPY config ./config` to bundle `policies.json` and email templates directly into the container.
- [.gitignore](file:///home/maqa/xsolla/.gitignore): Created root `.gitignore` to prevent secret manifests (`*secrets*.yaml`) and `.env` files from being committed.

### 2. Kubernetes Manifests Created

The manifests are organized in [`k8s/`](file:///home/maqa/xsolla/xsolla-capstone/k8s):

| File | Description |
| :--- | :--- |
| [01-secrets.yaml](file:///home/maqa/xsolla/k8s/01-secrets.yaml) | Secret with all database credentials, connection strings, JWT, Supabase S3, Google OAuth, Gemini, and RabbitMQ configs. |
| [01-secrets.example.yaml](file:///home/maqa/xsolla/k8s/01-secrets.example.yaml) | Sanitized template for version control. |
| [k8s/base/](file:///home/maqa/xsolla/xsolla-capstone/k8s/base) | Shared namespace, data layer, backends, frontends, and ingress resources. |
- [k8s/overlays/minikube/](file:///home/maqa/xsolla/k8s/overlays/minikube): Minikube overlay using local dev images.
- [k8s/overlays/prod/](file:///home/maqa/xsolla/k8s/overlays/prod): Production overlay applying TLS cert-manager (`letsencrypt-prod`).

---

## Deployment & Verification Guide

### Step 1: Build Images Inside Minikube

Point your Docker CLI to Minikube's Docker daemon and build all 5 images:

```bash
eval $(minikube docker-env)

# 1. LMS API
docker build -t xsolla-capstone-api:dev -f xsolla-capstone/Dockerfile xsolla-capstone/

# 2. Notification Worker (bundles config/policies.json & config/templates)
docker build -t xsolla-notification-worker:dev -f xsolla-capstone/notification-service/Dockerfile xsolla-capstone/notification-service/

# 3. Office Explorer Backend
docker build -t xsolla-office-explorer-api:dev -f xsolla-office-explorer-backend/backend/Dockerfile xsolla-office-explorer-backend/backend/

# 4. LMS Frontend (with build args)
docker build \
  --build-arg VITE_API_URL=http://api.lms.minikube.local \
  --build-arg VITE_OFFICE_TOUR_URL=http://office.minikube.local \
  -t xsolla-capstone-frontend:dev \
  -f xsolla-capstone-frontend/Dockerfile xsolla-capstone-frontend/

# 5. Office Explorer Frontend
docker build \
  -t xsolla-office-explorer-frontend:dev \
  -f xsolla-office-explorer-frontend/Dockerfile xsolla-office-explorer-frontend/
```

### Step 2: Apply the Manifests

Apply the canonical Minikube overlay:

```bash
kubectl apply -k k8s/overlays/minikube

# Wait for DB, broker, and services to reach Ready state
kubectl rollout status -n xsolla statefulset/postgres --timeout=120s
kubectl rollout status -n xsolla deployment/rabbitmq --timeout=120s
kubectl rollout status -n xsolla deployment/lms-frontend --timeout=120s
kubectl rollout status -n xsolla deployment/office-frontend --timeout=120s
```

For production, apply `k8s/overlays/prod` after supplying the required production secrets.

### Step 3: Verify the Deployment

```bash
# Verify all pods are Running (1/1)
kubectl get pods -n xsolla

# Verify PostgreSQL databases (lms, notify, xsolla_explorer)
kubectl exec -n xsolla postgres-0 -- psql -U postgres -l

# Verify Office Explorer tables are seeded
kubectl exec -n xsolla postgres-0 -- psql -U postgres -d xsolla_explorer -c "\dt"

# Test LMS API readiness endpoint via Ingress
curl -i http://api.lms.minikube.local/readyz

# Test Office Explorer API via Frontend Nginx reverse proxy
curl -i http://office.minikube.local/api/v1/explorer/init

# Test Frontends via Ingress
curl -i http://lms.minikube.local/
curl -i http://office.minikube.local/
```