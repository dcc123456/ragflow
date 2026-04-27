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
   uv sync --python 3.12 --all-extras
   uv run download_deps.py
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

# [ragflow_enterprise] recent context, 2026-04-25 6:14am UTC

Legend: 🎯session 🔴bugfix 🟣feature 🔄refactor ✅change 🔵discovery ⚖️decision 🚨security_alert 🔐security_note
Format: ID TIME TYPE TITLE
Fetch details: get_observations([IDs]) | Search: mem-search skill

Stats: 50 obs (13,325t read) | 521,358t work | 97% savings

### Apr 24, 2026
S26 Adjust billing-related fields in conf/service_conf.yaml to match docker/service_conf.yaml.template (some missing, some extraneous) (Apr 24, 2:26 AM)
S27 billing_webhook_secret stored in MySQL system_settings table (Apr 24, 2:26 AM)
S29 Investigating billing_webhook_secret in MySQL system_settings table for RAGFlow Enterprise billing configuration (Apr 24, 2:55 AM)
S31 Typo in Terraform variable validation blocks tofu apply (Apr 24, 2:55 AM)
S42 Points checkout refactored to use quantity-based API with dynamic pricing (Apr 24, 3:38 AM)
S44 Buy Credits dialog refactored to use backend-sourced price info and quantity-based checkout (Apr 24, 5:44 AM)
S56 Root cause: wrong function name in migration (Apr 24, 5:45 AM)
S57 Fix ragflow pod crash - NameError in db_models.py migration (Apr 24, 2:46 PM)
S69 Buy Credits payment success shows error dialog (Apr 24, 2:47 PM)
### Apr 25, 2026
64 5:08a 🔵 Buy Credits webhook flow and payment confirmation mechanism
66 5:09a 🔵 Points recharge webhook uses checkout.session.completed, not payment_intent.succeeded
67 " 🔵 Root cause: get_metadata_from_intent returns empty dict causing error dialog
68 5:11a 🔴 Buy Credits payment success shows error dialog
69 " 🔵 PointAccount.recharge uses idempotency pattern
70 5:12a 🔵 User rejects kubectl env check for BILLING_SERVICE_URL diagnosis
71 " 🔵 User disputes kubectl-based BILLING_SERVICE_URL diagnosis approach
72 5:13a 🔵 Ragflow pods not visible in stage cluster kubectl context
73 " 🔵 Ragflow pods found in ragflow namespace on stage cluster
74 " 🔵 Stage cluster ragflow pod billing environment variables discovered
75 5:14a 🔵 Correct service_conf.yaml path inside ragflow pod is /ragflow/conf/
77 " 🔵 Stage cluster billing configuration retrieved from /ragflow/conf/service_conf.yaml
78 " 🔵 Billing success redirect flow in billing_app.py analyzed
79 " 🔵 Tested billing success endpoint with invalid session_id
80 5:15a 🔵 Frontend price-pay-status handling in React components
83 5:17a 🔵 PointAccount.recharge idempotency pattern in billing_service.py
84 5:18a 🔵 Checkout session completed handler for points_recharge
86 5:20a 🔵 Stripe webhook endpoint configuration in stage cluster logs
88 5:21a 🔵 Only one checkout.session.completed reference in logs - webhook registration only
89 5:22a 🔵 Billing webhook signature verification flow in billing_app.py
91 5:23a 🔵 Webhook handler prints "Passed in {event_type}" on successful processing
92 5:24a 🔵 Stripe webhook secret stored in database system_settings table
93 5:26a 🔵 Billing webhook secret successfully retrieved from database
95 5:29a 🔵 Buy Credits payment succeeds but shows error dialog
96 5:30a 🔵 Billing success endpoint redirects to error on invalid session
98 5:31a 🔵 Stripe checkout sessions confirm successful payments
S112 Debug Buy Credits payment success showing error dialog despite successful Stripe payment (Apr 25, 5:32 AM)
99 5:43a 🔴 Buy Credits payment success shows error dialog despite successful Stripe payment
100 " 🔵 PointAccount.recharge uses idempotency pattern with payment_intent_id
101 " 🔵 No checkout.session.completed events processed since billing pod startup
102 5:44a 🔴 Buy Credits payment success shows error dialog despite successful Stripe payment
103 " 🔵 No checkout.session.completed webhook events processed since pod startup
104 " 🔵 Stripe webhook URL configuration and event delivery verification needed
105 " 🔵 Payment success modal logic and redirect flow traced
109 5:45a 🔵 Points payments ARE succeeding - credits are being added to PointAccount
110 " 🔵 Billing success/cancel URLs configured to localhost in deployed service_conf.yaml
111 " 🔵 Frontend success modal has inverted title/content for unrecognized payment status
112 5:46a 🔵 No checkout.session.completed webhook events processed since pod startup
113 " 🔵 Success modal logic in pricing-plan/index.tsx maps status to title/content
114 " 🔵 PointAccount.recharge idempotency pattern uses idempotency_key
115 5:47a 🔵 GKE cluster context: gke_ragflow-stage_us-east1_stage-cluster-1
117 " 🔵 Webhook signature verification failing on pod kkfkw
118 " 🔵 Empty metadata warning in payment_intent.succeeded handler
119 " 🔵 /v1/billing/success endpoint returns 302 redirect
121 5:51a 🔴 Added session_id query parameter to Stripe checkout success URLs
122 " 🔵 Stripe webhook signature verification fails on replica kkfkw
123 " 🔵 Empty metadata in payment_intent.succeeded causes recharge skip
124 " 🔴 Applied _build_checkout_success_url across all Stripe checkout paths
125 " 🔴 /success endpoint now returns error status instead of cancel for missing session_id
126 5:53a 🔴 Stripe checkout success URL now includes session ID for frontend verification
127 " 🔴 Buy Credits payment modal now displays correct title and content

Access 521k tokens of past work via get_observations([IDs]) or mem-search skill.
</claude-mem-context>