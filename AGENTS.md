# RAGFlow Project Instructions

This file provides comprehensive guidance for working with the RAGFlow project, serving both GitHub Copilot and Claude Code.

**Note for Claude Code Users**: CLAUDE.md is a symlink to this file. Claude Code may automatically update certain sections, but the "Project Context & Lessons Learned" section (section 11) is reserved for human-curated content.

## 1. Project Overview

RAGFlow is an open-source RAG (Retrieval-Augmented Generation) engine based on deep document understanding. It is a full-stack application with a Python backend and a React/TypeScript frontend.

**Technology Stack**:
- **Backend**: Python 3.12+ (Flask/Quart API server)
- **Frontend**: TypeScript, React, UmiJS framework
- **Architecture**: Microservices based on Docker deployment
- **Data Stores**: MySQL, Elasticsearch/Infinity, Redis, MinIO
- **Package Manager**: uv for Python dependencies, npm for frontend

## 2. Directory Structure
- `api/`: Backend API server (Flask/Quart)
  - `apps/`: API Blueprints for different functionalities
    - `kb_app.py` - Knowledge base management
    - `dialog_app.py` - Chat/conversation handling
    - `document_app.py` - Document processing
    - `canvas_app.py` - Agent workflow canvas
    - `file_app.py` - File upload/management
  - `db/`: Database models and services
    - `db_models.py` - Database models
    - `services/` - Business logic services
- `rag/`: Core RAG logic
  - `llm/`: LLM, Embedding, and Rerank model abstractions
  - `flow/`: Chunking, parsing, tokenization pipeline
  - `rag/graphrag/`: Knowledge graph construction and querying
- `deepdoc/`: Document parsing and OCR modules (PDF parsing, layout analysis)
- `agent/`: Agentic reasoning components
  - `components/`: Modular workflow components (LLM, retrieval, categorize, etc.)
  - `templates/`: Pre-built agent workflows
  - `tools/`: External API integrations (Tavily, Wikipedia, SQL execution, etc.)
- `web/`: Frontend application (React + UmiJS)
  - React/TypeScript with UmiJS framework
  - Ant Design + shadcn/ui components
  - State management with Zustand
  - Tailwind CSS for styling
- `docker/`: Docker deployment configurations
- `sdk/`: Python SDK for API integration
- `test/`: Backend tests
- `pulumi_ragflow/`: Kubernetes deployment using Pulumi

## 3. Build Instructions

### Backend (Python)
The project uses **uv** for dependency management.

1. **Setup Environment**:
   ```bash
   uv sync --python 3.13 --all-extras
   uv run python3 download_deps.py
   ```

2. **Run Server**:
   - **Pre-requisite**: Start dependent services (MySQL, ES/Infinity, Redis, MinIO).
     ```bash
     docker compose -f docker/docker-compose-base.yml up -d
     ```
   - **Launch**:
     ```bash
     source .venv/bin/activate
     export PYTHONPATH=$(pwd)
     bash docker/launch_backend_service.sh
     ```

### Frontend (TypeScript/React)
Located in `web/`.

1. **Install Dependencies**:
   ```bash
   cd web
   npm install
   ```

2. **Run Dev Server**:
   ```bash
   npm run dev
   ```
   Runs on port 8000 by default.

### Docker Deployment
To run the full stack using Docker:
```bash
cd docker
docker compose -f docker-compose.yml up -d
```

## 4. Common Development Commands

### Backend Development

#### Setup Environment
```bash
# Install Python dependencies using uv
uv sync --python 3.12 --all-extras
uv run download_deps.py

# Install pre-commit hooks
pre-commit install
```

#### Start Dependent Services
```bash
# Start MySQL, Elasticsearch/Infinity, Redis, MinIO
docker compose -f docker/docker-compose-base.yml up -d
```

#### Run Backend Server
```bash
# Activate virtual environment and set PYTHONPATH
source .venv/bin/activate
export PYTHONPATH=$(pwd)

# Launch backend service
bash docker/launch_backend_service.sh
```

#### Linting and Formatting
```bash
# Check code with ruff
ruff check

# Format code with ruff
ruff format

# Run pre-commit hooks on all files
pre-commit run --all-files
```

### Frontend Development

#### Install Dependencies
```bash
cd web
npm install
```

#### Run Development Server
```bash
npm run dev        # Runs on port 8000 by default
```

#### Build for Production
```bash
npm run build      # Production build
```

#### Testing and Linting
```bash
npm run test       # Jest tests
npm run lint       # ESLint
```

### Docker Development
```bash
# Full stack with Docker
cd docker
docker compose -f docker-compose.yml up -d

# Check server status
docker logs -f ragflow-server

# Rebuild images
docker build --platform linux/amd64 -f Dockerfile -t infiniflow/ragflow:nightly .
```

### Kubernetes Deployment (Pulumi)
RAGFlow supports deployment to Kubernetes clusters using Pulumi as an alternative to traditional Helm chart deployment.

#### Prerequisites
1. **Pulumi CLI**: Install from [https://www.pulumi.com/docs/install/](https://www.pulumi.com/docs/install/)
2. **Go 1.24+**: Install from [https://golang.org/dl/](https://golang.org/dl/)
3. **Kubernetes CLI**: `kubectl` configured to access your cluster
4. **Gateway API**: Requires installation of either Cilium or NGINX Gateway (see below)

#### Deployment Steps
```bash
# Navigate to Pulumi directory
cd pulumi_ragflow

# Install Go dependencies
go mod download

# Install Pulumi Kubernetes provider
pulumi plugin install resource kubernetes v4.24.1

# Setup Gateway API (choose one)
# Use Cilium Gateway (recommended if cluster has Cilium CNI installed)
./setup-cilium-gateway.sh

# Or use NGINX Gateway
./setup-nginx-gateway.sh

# Initialize Pulumi stack
pulumi stack init dev

# Configure environment variables (optional)
export PULUMI_NAMESPACE="ragflow-prod"  # Custom namespace
export RAGFLOW_GATEWAY="ragflow.local"  # Gateway hostname

# Preview deployment
pulumi preview

# Execute deployment
pulumi up

# Verify deployment
kubectl get pods -n ragflow
kubectl get services -n ragflow
kubectl get gateway -A
```

#### Important Configuration Notes
1. **HOST_ADDRESS Environment Variable**: Required for testing with correct API address
   - Default: `http://127.0.0.1:9380` (local development)
   - k8s deployment: Use Gateway hostname, e.g., `http://ragflow.local`
   - Set via: `export HOST_ADDRESS=http://ragflow.local`

2. **Gateway API Requirement**: Must install either Cilium or NGINX Gateway
   - Cilium Gateway: Deep integration with Cilium CNI, better performance
   - NGINX Gateway: Full-featured implementation, broad community support

3. **DNS/Hosts Configuration**: If `RAGFLOW_GATEWAY` hostname is not managed by DNS, add to `/etc/hosts`:
   ```bash
   echo "<gateway-ip> ragflow.local" | sudo tee -a /etc/hosts
   ```


#### Resource Cleanup
```bash
# Destroy deployed resources
pulumi destroy

# Remove stack
pulumi stack rm dev
```

## 5. Testing Instructions

*For detailed test execution guide, please refer to the "Test Execution Guide" section in Section 8 "Testing Details".*

## 6. Coding Standards & Guidelines

### Language Usage
- **Documentation**: Use English for all documentation files (CLAUDE.md, README.md, etc.)
- **Code comments**: Prefer English for code comments and docstrings
- **Consistency**: Maintain consistent language usage throughout the codebase
- **Rationale**: English facilitates global collaboration and works best with AI assistants

### Python Code Quality
- **Linting**: Use `ruff` for code quality checks
  ```bash
  ruff check
  ```
- **Formatting**: Use `ruff` for code formatting
  ```bash
  ruff format
  ```
- **Pre-commit Hooks**: Install and run pre-commit hooks
  ```bash
  pre-commit install
  pre-commit run --all-files
  ```

### Frontend Code Quality
- **Linting**: Use ESLint for TypeScript/JavaScript code
  ```bash
  cd web
  npm run lint
  ```

### General Development Practices
- Write clear, descriptive commit messages
- Include tests for new functionality
- Update documentation when changing behavior
- Use consistent naming conventions
- Follow established project patterns and architecture

## 7. Key Configuration Files
- `docker/.env` - Environment variables for Docker deployment
- `docker/service_conf.yaml.template` - Backend service configuration
- `pyproject.toml` - Python dependencies and project configuration
- `web/package.json` - Frontend dependencies and scripts
- `pulumi_ragflow/main.go` - Pulumi k8s deployment configuration (Go language)
- `pulumi_ragflow/Pulumi.yaml` - Pulumi project configuration
- `pulumi_ragflow/README.md` - Pulumi deployment detailed documentation

## 8. Testing Details

### Test Execution Guide

#### 1. Test Environment Preparation
**All tests require correct `HOST_ADDRESS` environment variable**:
- **Local development**: `export HOST_ADDRESS=http://127.0.0.1:9380`
- **Docker deployment**: `export HOST_ADDRESS=http://localhost:9380`
- **k8s deployment**: `export HOST_ADDRESS=http://<gateway-hostname>` (e.g., `http://ragflow.local`)

**Special requirements for k8s deployment**:
1. Ensure Gateway API is working properly (Cilium or NGINX Gateway)
2. If gateway hostname is not managed by DNS, add to `/etc/hosts`:
   ```bash
   echo "<gateway-ip> ragflow.local" | sudo tee -a /etc/hosts
   ```
3. Ensure services are accessible through the gateway

#### 2. Backend Tests (Python/pytest)
**Install test dependencies**:
```bash
uv venv --python 3.12
uv sync --python 3.12 --only-group test --no-default-groups --frozen
uv pip install sdk/python --group test
```

**Run tests**:
```bash
# Activate virtual environment
source .venv/bin/activate

# Run all tests
pytest

# Run specific test file
pytest test/test_api.py

# Run HTTP API tests
pytest test/testcases/test_http_api/ -v

# Run SDK tests
pytest sdk/python/test/test_sdk_api/ -v

# Run tests with verbose output
pytest -v

# Run tests using uv
uv run pytest
```

#### 3. Frontend Tests (TypeScript/Jest)
```bash
cd web
npm run test
```

#### 4. Test Locations
- **Backend tests**: `test/` directory
- **HTTP API tests**: `test/testcases/test_http_api/`
- **SDK tests**: `sdk/python/test/test_sdk_api/`
- **Web API tests**: `test/testcases/test_web_api/`
- **Frontend tests**: Test files in `web/` directory

#### 5. Test Configuration Files
- `test/testcases/configs.py` - HTTP API test configuration
- `sdk/python/test/conftest.py` - SDK test configuration
- `sdk/python/test/test_http_api/conftest.py` - HTTP API test configuration

#### 6. DeepDoc GPU Service Testing

DeepDoc provides OCR, DLA (Document Layout Analysis), and TSR (Table Structure Recognition) services.

**Service Endpoints**:
| Endpoint | Method | Function | GPU Required |
|---------|--------|----------|--------------|
| `/health` | GET | Health check | No |
| `/predict/ocr` | POST | Text detection and recognition | No |
| `/predict/dla` | POST | Document layout analysis | Yes |
| `/predict/tsr` | POST | Table structure recognition | Yes |

**GPU Version Requirements**:
- **CUDA Driver**: 535+ (supports CUDA 12.1)
- **TensorRT**: 8.6.3.1 (compatible with CUDA 12.0/12.1 and Driver 535)
- **Important**: TensorRT 10.x requires CUDA 12.6+ Driver (550+), not compatible with Driver 535

**Testing DeepDoc Service**:
```bash
# Run all tests
python3 deepdoc/servers/deepdoc_test.py --url http://localhost:8000 --service all

# Test individual services
python3 deepdoc/servers/deepdoc_test.py --url http://localhost:8000 --service ocr
python3 deepdoc/servers/deepdoc_test.py --url http://localhost:8000 --service dla
python3 deepdoc/servers/deepdoc_test.py --url http://localhost:8000 --service tsr
```

**Common Issues**:
1. **CUDA initialization failure (error: 35)**: TensorRT/CUDA version mismatch with GPU driver
   - Solution: Use CUDA 12.1 + TensorRT 8.6.3 for Driver 535
2. **OCR returns empty results**: Image color space issue
   - Solution: Use `cv2.IMREAD_COLOR` to ensure 3-channel RGB input
3. **DLA/TSR batch dimension errors**: LitServe adds batch dimension
   - Solution: Check `x.ndim` to handle both single and batched images

**Performance Reference**:
- GPU Version: OCR ~100-300ms, DLA/TSR ~50-150ms, GPU memory ~8-10GB
- CPU Version: OCR ~500-2000ms, only OCR supported


## 9. Database Engines
RAGFlow supports switching between Elasticsearch (default) and Infinity:
- Set `DOC_ENGINE=infinity` in `docker/.env` to use Infinity
- Requires container restart: `docker compose down -v && docker compose up -d`

## 10. Development Environment Requirements

### Basic Development Environment
- Python 3.12
- Node.js >=18.20.4
- Docker & Docker Compose
- uv package manager
- 16GB+ RAM, 50GB+ disk space

### Additional Requirements for k8s Deployment
- **Pulumi CLI**: For Infrastructure as Code deployment
- **Go 1.24+**: Pulumi project is written in Go
- **Kubernetes Cluster**: v1.24+ with kubectl access configured
- **Gateway API**: Requires Cilium or NGINX Gateway support
- **Helm**: For installing Gateway API components


<claude-mem-context>
# Memory Context

# [ragflow_enterprise/billing-refactor-9smmy] recent context, 2026-05-25 5:45pm GMT+8

Legend: 🎯session 🔴bugfix 🟣feature 🔄refactor ✅change 🔵discovery ⚖️decision 🚨security_alert 🔐security_note
Format: ID TIME TYPE TITLE
Fetch details: get_observations([IDs]) | Search: mem-search skill

Stats: 50 obs (14,413t read) | 2,091,054t work | 99% savings

### May 24, 2026
S1228 CI runner credentials migration — replace GitHub secrets with runner env vars (May 24, 8:11 PM)
S1279 Fix CI failures on billing-refactor branch (PR #533) - KeyError: 'data' in chat assistant tests (May 24, 8:17 PM)
S1301 Fix test fixture batch_create_chat_assistants exceeding BILLING_QUOTA_TRIAL_APPS quota (May 24, 10:23 PM)
S1306 batch_create_chat_assistants quota clamping risks misleading callers (May 24, 10:40 PM)
S1308 batch_create_chat_assistants return signature fix: tuple with quota metadata (May 24, 10:43 PM)
S1394 PR #533 draft-to-review CI trigger failure investigation (May 24, 10:43 PM)
### May 25, 2026
S1465 PR #533 draft→ready transition not triggering CI (May 25, 10:07 AM)
S1466 PR #533 draft→ready not triggering CI — traced to tests.yml syntax error in commit 615c3e7f9 (May 25, 10:31 AM)
S1469 PR #533 CI not triggering — tests.yml env.KUBE_CONFIG syntax error confirmed (May 25, 10:32 AM)
S1472 Fix pushed to branch for PR #533 CI workflow syntax error (May 25, 10:35 AM)
1068 11:03a ✅ PR #533 CI: Refined bridge step — writes to both GITHUB_ENV and GITHUB_OUTPUT, references via step outputs
1069 11:10a 🔵 PR #533 CI: The same BYOK kubeconfig error persists after workflow changes — fix may not be applied to correct job
1070 11:11a 🔵 PR #533 CI: ragflow_tests_byok is a separate job, not part of ragflow_tests — bridge step not applied to it
1071 11:35a ✅ CI workflow GH_PAT secret replaced with MY_GH_TOKEN runner env var
1073 11:38a 🔴 CI workflow now uses MY_GH_TOKEN runner env var instead of GH_PAT secret
1075 11:42a 🔴 CI workflow GH_PAT secret replaced with MY_GH_TOKEN runner env var
1078 11:50a 🔴 PR #533 CI BYOK jobs: GH_PAT replaced with MY_GH_TOKEN runner env var
1079 " ✅ Elasticsearch test log collection changed from !cancelled() to always()
1080 " ✅ Two commits pending push on billing-refactor-9smmy branch
1081 11:56a 🔴 PR #533 CI still failing: kubeconfig input not supplied after bridge_ci_env fix
1083 12:02p 🔴 Merge conflict resolved: kubeconfig now uses bridge_ci_env output
1084 12:52p 🔵 tests.yml Set Kubernetes Context step present but Python assertion logic flawed
1085 12:53p 🔵 Pytest test level and client type infrastructure discovered
1086 12:55p 🔵 Local ragflow-1 container fails due to missing GitHub runner registration token
1087 1:19p ✅ Built infiniflow-ai/ragflow:latest Docker image
1088 " 🔵 HTTP API pytest suite testing plan initiated
1091 1:22p 🔵 RAGFlow container logs reveal port binding and health issues
1092 " 🔵 RAGFlow HTTP API pytest passed with local Docker stack
1093 " 🔵 RAGFlow API endpoints and JWT parsing warnings during pytest
1094 1:27p 🔵 CI workflow env configuration discovered for local match
1095 1:28p ✅ Added tei-cpu compose profile to local docker/.env
1096 " ✅ Added tei-cpu compose profile and TEI_MODEL to docker/.env
1099 1:37p 🔵 RabbitMQ management port mismatch: .env uses 1673, actual service exposes 15672
1100 1:40p 🔴 admin_metrics.py now reads RabbitMQ config from settings.RABBIT_CONF instead of env vars
1101 1:43p 🔵 Dockerfile git describe fails in worktree builds, VERSION file ends up empty
1102 1:45p 🔴 RAGFlow server fails to start because DOC_ENGINE env var contains literal shell substitution syntax
1103 " 🔵 docker-compose .env does not support shell default syntax ${VAR:-default} for custom variables
1104 1:48p 🔵 Multiple .env variables use shell default syntax that docker-compose passes through literally
1105 1:49p 🔴 admin_metrics.py RabbitMQ fix confirmed: metrics collection now works in locally-built Docker image
1106 1:50p 🔵 RAGFlow API server (9380) and admin metrics (9381) both returning HTTP 200
1108 2:06p 🔵 RabbitMQ container IP discovered at 172.18.0.3
1110 2:09p 🔵 RabbitMQ lazy-init fix unblocks billing quota enforcement
1111 2:14p ⚖️ Third test resource cleanup approach: immediate deletion without affecting test logic
1113 2:15p 🔵 Billing subscription overview API returns 404 with /api/v1 prefix
1114 2:22p 🔵 Billing subscription overview endpoint works at correct path without /api prefix
1115 2:30p ⚖️ BILLING_QUOTA_TRIAL_APPS=3 to preserve test logic
1117 2:31p ✅ BILLING_QUOTA_TRIAL_APPS bumped from 1 to 3 across config, CI, and docs
1122 2:39p 🔴 S3 storage NoSuchBucket causes document parsing timeout
1124 2:40p 🔵 RAGFlow container uses MINIO storage (not S3) for document uploads
1127 2:44p ✅ Trial apps quota bumped from 1 to 3 across env, CI, and docs
1128 " 🔵 test_metadata_retrieval.py parsing timeout investigated — RTK proxy vs direct pytest difference
1130 2:45p 🔵 test_metadata_retrieval.py fails in full suite run but passes individually
1131 2:46p 🔴 test_metadata_retrieval.py ImportError root cause identified
1132 " 🔴 test_metadata_retrieval.py import fix: moved upload_documents/parse_documents/retrieval_chunks to top-level
1135 2:48p 🔴 test_metadata_retrieval.py import fix verified in full p2 suite rerun
1136 2:49p 🔴 Full p2 suite rerun passed: 806 passed, 97 skipped, 0 failures
1143 2:52p 🔴 BYOK CI kubeconfig unavailable in pull_request context — azure/k8s-set-context fails
1146 " ✅ BILLING_QUOTA_STARTER_APPS raised from 3 to 4 across CI, env, and docs
1147 " 🔄 RabbitMQ connection management refactored to lazy _ensure_connection() pattern
1148 " ✅ docker/.env tei-cpu compose profile and TEI_MODEL variable added

Access 2091k tokens of past work via get_observations([IDs]) or mem-search skill.
</claude-mem-context>