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

# [ragflow_enterprise] recent context, 2026-04-28 3:56am UTC

Legend: 🎯session 🔴bugfix 🟣feature 🔄refactor ✅change 🔵discovery ⚖️decision 🚨security_alert 🔐security_note
Format: ID TIME TYPE TITLE
Fetch details: get_observations([IDs]) | Search: mem-search skill

Stats: 50 obs (12,880t read) | 433,440t work | 97% savings

### Apr 25, 2026
117 5:47a 🔵 Webhook signature verification failing on pod kkfkw
121 5:51a 🔴 Added session_id query parameter to Stripe checkout success URLs
122 " 🔵 Stripe webhook signature verification fails on replica kkfkw
123 " 🔵 Empty metadata in payment_intent.succeeded causes recharge skip
124 " 🔴 Applied _build_checkout_success_url across all Stripe checkout paths
125 " 🔴 /success endpoint now returns error status instead of cancel for missing session_id
126 5:53a 🔴 Stripe checkout success URL now includes session ID for frontend verification
127 " 🔴 Buy Credits payment modal now displays correct title and content
### Apr 27, 2026
133 1:39a ✅ Amended billing split commit message from feat to fix
134 1:55a 🔵 PR #362 review comments reveal 15 code issues
136 1:56a 🔵 PriceType/ProductType enums confirmed as ADDON not USAGE_BASED
137 1:57a 🔵 DB model choices use 'usage_based' while enum uses 'addon'
138 " 🔴 PR #362 review comments fixed in billing_app.py and init_data.py
139 3:09a 🔵 CI build failure on deepdoc Dockerfile
140 3:10a 🔵 PointAccount sync_plan_points_on_subscription_paid is idempotent
141 " 🔵 CI failure in ragflow_enterprise deepdoc Dockerfile
S140 Fix CI test failure test_invoice_paid_updates_existing_failed_order_and_restores_active in ragflow_enterprise (Apr 27, 3:18 AM)
144 3:18a 🔵 uv sync fails due to graspologic gitee mirror SSL timeout
S141 CI failure investigation on ragflow_enterprise GitHub Actions (Apr 27, 3:24 AM)
145 3:25a 🔵 CI failure investigation on ragflow_enterprise GitHub Actions
S142 PDF batch processing reduces parser peak memory (Apr 27, 3:25 AM)
146 3:35a 🟣 Batch PDF parsing reduces parser peak memory
147 " ✅ OCR memory optimization with lazy numpy view creation
148 " 🟣 DEEPDOC_URL mode skips local Layout ONNX model initialization
149 " ✅ DLA serialization JPEG bytes generated once and reused
150 " 🔵 Memory optimization verification blocked by graspologic dependency timeout
151 3:36a 🟣 PDF batch processing reduces parser peak memory
152 " ✅ OCR temporary memory optimization
153 " ✅ DEEPDOC_URL mode skips local Layout ONNX initialization
154 " ✅ DLA serialization reuses JPEG bytes to reduce allocations
S143 billing_all_plans API to include quota_points per plan (Apr 27, 3:36 AM)
155 5:44a 🟣 billing_all_plans needs to return quota_points per plan
156 " 🟣 billing_all_plans API to include quota_points per plan
S152 Investigating billing_webhook signature verification: determine if failed signature check crashes the process or returns 400 correctly (Apr 27, 5:44 AM)
157 6:18a 🔵 Claude tool call failures traced to bwrap loopback sandbox restriction
158 6:19a 🔵 ragflow_enterprise has MCP tool-call infrastructure in common/mcp_tool_call_conn.py
160 7:25a 🔴 Stripe webhook signature verification failing in ragflow pod
162 7:26a 🔵 Billing webhook secret fetched dynamically from database with in-memory cache
S156 Storage checkout backend returns redirect_to correctly but frontend not processing it (Apr 27, 7:28 AM)
163 7:29a 🔴 MCP smart_search tool calls failing in ragflow_enterprise
164 " 🔵 MySQL root access denied - ragflow user credentials work for database queries
165 7:48a 🔵 Stripe webhook secret verified matching between dashboard and database
166 8:09a 🔴 Buy Storage not redirecting to Stripe payment page
167 " 🔵 Storage checkout returns redirect_to field in response
169 " 🔵 Storage checkout returns different response formats per scenario
170 8:10a 🔵 Storage checkout backend returns redirect_to correctly but frontend not processing it
S179 Implement idempotency for subscription cancellation when managed by subscription schedule (Apr 27, 8:10 AM)
173 8:40a 🔴 Switched parse_storage_size from binary to decimal units
175 8:41a 🔴 Aligned convertBytesToGb frontend to use decimal GB units
176 " 🔵 Three more 1024-based BYTES_PER_GB constants found in billing code
177 8:42a 🔴 Aligned all storage quota conversions to decimal (1000) units
178 8:43a 🔵 formatBytes uses inconsistent units after decimal conversion
179 9:20a 🟣 Subscription cancellation idempotency for subscription schedules
S181 Rewrite HEAD commit message to describe the actual billing fix for delinquent subscription handling (Apr 27, 9:20 AM)
### Apr 28, 2026
S184 Create PR from stage branch to saas branch on origin with auto-generated title and body (Apr 28, 1:43 AM)
182 3:41a 🔄 billing_points_checkout now extracts session success/cancel URLs like billing_checkout
S187 billing_points_checkout now extracts session success/cancel URLs like billing_checkout (Apr 28, 3:41 AM)
183 3:42a 🔄 billing_points_checkout should extract URL params like billing_checkout
184 3:43a 🔄 billing_points_checkout added session_success_url and session_cancel_url parameter extraction
185 3:48a 🔄 billing_points_checkout aligned with billing_checkout URL extraction

Access 433k tokens of past work via get_observations([IDs]) or mem-search skill.
</claude-mem-context>