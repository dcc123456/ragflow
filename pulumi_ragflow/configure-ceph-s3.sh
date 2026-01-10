#!/bin/bash
# configure-ceph-s3.sh
# 检查并配置 Ceph RGW S3 账户、bucket 等信息
# 然后将配置保存到 Pulumi 配置中

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Ceph RGW S3 配置脚本 ===${NC}"

# 检查必要的命令
check_command() {
    if ! command -v $1 &> /dev/null; then
        echo -e "${RED}错误: 未找到命令 '$1'${NC}"
        exit 1
    fi
}

check_command kubectl
check_command pulumi

# 检查当前目录
if [ ! -f "main.go" ]; then
    echo -e "${YELLOW}警告: 不在 pulumi_ragflow 目录中，尝试切换到正确目录...${NC}"
    cd "$(dirname "$0")"
    if [ ! -f "main.go" ]; then
        echo -e "${RED}错误: 请在 pulumi_ragflow 目录中运行此脚本${NC}"
        exit 1
    fi
fi

# 配置参数
S3_USER="ragflow"
S3_DISPLAY_NAME="RAGFlow User"
S3_EMAIL="ragflow@example.com"
S3_BUCKET="ragflow"
S3_REGION="us-east-1"
S3_ENDPOINT="http://rook-ceph-rgw-my-store.rook-ceph.svc:80"
CEPH_NAMESPACE="rook-ceph"
TOOLS_DEPLOYMENT="rook-ceph-tools"

echo -e "${GREEN}1. 检查 Ceph RGW 服务...${NC}"

# 检查 Ceph RGW 服务
if ! kubectl get service rook-ceph-rgw-my-store -n $CEPH_NAMESPACE &> /dev/null; then
    echo -e "${RED}错误: 未找到 Ceph RGW 服务 'rook-ceph-rgw-my-store'${NC}"
    echo "请确保 Ceph RGW 已正确部署在命名空间 $CEPH_NAMESPACE 中"
    exit 1
fi

echo -e "${GREEN}✓ Ceph RGW 服务正常${NC}"

# 检查 rook-ceph-tools pod
echo -e "${GREEN}2. 检查 rook-ceph-tools pod...${NC}"
if ! kubectl get deployment $TOOLS_DEPLOYMENT -n $CEPH_NAMESPACE &> /dev/null; then
    echo -e "${YELLOW}警告: 未找到 rook-ceph-tools deployment，尝试创建...${NC}"

    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $TOOLS_DEPLOYMENT
  namespace: $CEPH_NAMESPACE
spec:
  replicas: 1
  selector:
    matchLabels:
      app: rook-ceph-tools
  template:
    metadata:
      labels:
        app: rook-ceph-tools
    spec:
      dnsPolicy: ClusterFirstWithHostNet
      containers:
      - name: rook-ceph-tools
        image: rook/ceph:v1.14.0
        command: ["/tini"]
        args: ["-g", "--", "/usr/local/bin/toolbox.sh"]
        imagePullPolicy: IfNotPresent
        env:
          - name: ROOK_CEPH_USERNAME
            valueFrom:
              secretKeyRef:
                name: rook-ceph-mon
                key: ceph-username
          - name: ROOK_CEPH_SECRET
            valueFrom:
              secretKeyRef:
                name: rook-ceph-mon
                key: ceph-secret
        securityContext:
          privileged: true
        volumeMounts:
          - mountPath: /dev
            name: dev
          - mountPath: /sys/bus
            name: sysbus
          - mountPath: /lib/modules
            name: libmodules
          - name: mon-endpoint-volume
            mountPath: /etc/rook
      volumes:
        - name: dev
          hostPath:
            path: /dev
        - name: sysbus
          hostPath:
            path: /sys/bus
        - name: libmodules
          hostPath:
            path: /lib/modules
        - name: mon-endpoint-volume
          configMap:
            name: rook-ceph-mon-endpoints
            items:
            - key: data
              path: mon-endpoints
EOF

    echo -e "${GREEN}等待 rook-ceph-tools pod 就绪...${NC}"
    sleep 10
    kubectl wait --for=condition=ready pod -n $CEPH_NAMESPACE -l app=rook-ceph-tools --timeout=60s
fi

echo -e "${GREEN}✓ rook-ceph-tools pod 正常${NC}"

echo -e "${GREEN}3. 检查/创建 S3 用户 '$S3_USER'...${NC}"

# 检查用户是否已存在
USER_EXISTS=$(kubectl exec -n $CEPH_NAMESPACE deployment/$TOOLS_DEPLOYMENT -- radosgw-admin user list --format=json 2>/dev/null | grep -o "\"$S3_USER\"" || true)

if [ -z "$USER_EXISTS" ]; then
    echo -e "${YELLOW}用户 '$S3_USER' 不存在，正在创建...${NC}"

    # 创建用户
    USER_JSON=$(kubectl exec -n $CEPH_NAMESPACE deployment/$TOOLS_DEPLOYMENT -- radosgw-admin user create \
        --uid="$S3_USER" \
        --display-name="$S3_DISPLAY_NAME" \
        --email="$S3_EMAIL" \
        --format=json 2>/dev/null)

    if [ $? -ne 0 ]; then
        echo -e "${RED}错误: 创建用户失败${NC}"
        exit 1
    fi

    echo -e "${GREEN}✓ 用户创建成功${NC}"
else
    echo -e "${GREEN}用户 '$S3_USER' 已存在，获取用户信息...${NC}"
    USER_JSON=$(kubectl exec -n $CEPH_NAMESPACE deployment/$TOOLS_DEPLOYMENT -- radosgw-admin user info --uid="$S3_USER" --format=json 2>/dev/null)
fi

# 提取访问密钥和秘密密钥
S3_ACCESS_KEY=$(echo "$USER_JSON" | grep -o '"access_key":"[^"]*"' | cut -d'"' -f4 | head -1)
S3_SECRET_KEY=$(echo "$USER_JSON" | grep -o '"secret_key":"[^"]*"' | cut -d'"' -f4 | head -1)

if [ -z "$S3_ACCESS_KEY" ] || [ -z "$S3_SECRET_KEY" ]; then
    echo -e "${RED}错误: 无法从用户信息中提取访问密钥${NC}"
    echo "用户信息:"
    echo "$USER_JSON"
    exit 1
fi

echo -e "${GREEN}✓ 获取到访问密钥${NC}"
echo -e "  Access Key: ${YELLOW}$S3_ACCESS_KEY${NC}"
echo -e "  Secret Key: ${YELLOW}********${NC}"

echo -e "${GREEN}4. 检查/创建 S3 bucket '$S3_BUCKET'...${NC}"

# 测试 S3 端点连通性
echo -e "测试 S3 端点连通性: $S3_ENDPOINT"
if ! kubectl run s3-test-$(date +%s) --rm -i --tty --image curlimages/curl --restart=Never -- \
    curl -s -f "$S3_ENDPOINT" >/dev/null 2>&1; then
    echo -e "${YELLOW}警告: S3 端点暂时不可达，bucket 创建将在部署时进行${NC}"
else
    echo -e "${GREEN}✓ S3 端点可达${NC}"

    # 尝试创建 bucket（使用临时 pod）
    echo -e "尝试创建 bucket '$S3_BUCKET'..."
    kubectl run s3-bucket-create-$(date +%s) --rm -i --tty --image amazon/aws-cli --restart=Never -- \
        bash -c "
        AWS_ACCESS_KEY_ID='$S3_ACCESS_KEY' \
        AWS_SECRET_ACCESS_KEY='$S3_SECRET_KEY' \
        AWS_DEFAULT_REGION='$S3_REGION' \
        AWS_ENDPOINT_URL='$S3_ENDPOINT' \
        aws --endpoint-url='$S3_ENDPOINT' s3api create-bucket --bucket '$S3_BUCKET' --region '$S3_REGION' || \
        aws --endpoint-url='$S3_ENDPOINT' s3 mb s3://'$S3_BUCKET' || \
        echo 'Bucket may already exist or creation failed'
        " 2>/dev/null && echo -e "${GREEN}✓ Bucket 创建尝试完成${NC}"
fi

echo -e "${GREEN}5. 保存配置到 Pulumi...${NC}"

# 检查是否在 Pulumi 项目中
if [ ! -f "Pulumi.yaml" ]; then
    echo -e "${YELLOW}警告: 未找到 Pulumi.yaml，跳过配置保存${NC}"
    echo -e "${YELLOW}请手动设置以下环境变量:${NC}"
    echo "export S3_ACCESS_KEY='$S3_ACCESS_KEY'"
    echo "export S3_SECRET_KEY='$S3_SECRET_KEY'"
    echo "export S3_BUCKET='$S3_BUCKET'"
    echo "export S3_REGION='$S3_REGION'"
    echo "export S3_ENDPOINT='$S3_ENDPOINT'"
    echo "export STORAGE_IMPL_TYPE='AWS_S3'"
else
    # 获取当前 stack
    CURRENT_STACK=$(pulumi stack --show-name 2>/dev/null || echo "dev")

    echo -e "当前 Pulumi stack: ${YELLOW}$CURRENT_STACK${NC}"

    # 设置非敏感配置
    echo -e "设置非敏感配置..."
    pulumi config set s3_bucket "$S3_BUCKET" 2>/dev/null || true
    pulumi config set s3_region "$S3_REGION" 2>/dev/null || true
    pulumi config set s3_endpoint "$S3_ENDPOINT" 2>/dev/null || true
    pulumi config set storage_impl_type "AWS_S3" 2>/dev/null || true

    # 设置敏感配置（使用 --secret）
    echo -e "设置敏感配置（加密存储）..."
    pulumi config set --secret s3_access_key "$S3_ACCESS_KEY" 2>/dev/null || true
    pulumi config set --secret s3_secret_key "$S3_SECRET_KEY" 2>/dev/null || true

    echo -e "${GREEN}✓ 配置已保存到 Pulumi stack '$CURRENT_STACK'${NC}"

    # 显示配置
    echo -e "\n${GREEN}当前配置:${NC}"
    pulumi config 2>/dev/null | grep -E "(s3_|storage_impl_type)" || true
fi

echo -e "\n${GREEN}6. 验证配置...${NC}"

# 创建测试配置验证脚本
cat > /tmp/verify-s3-config.sh << 'EOF'
#!/bin/bash
set -e

echo "验证 S3 配置..."
echo "Endpoint: $S3_ENDPOINT"
echo "Bucket: $S3_BUCKET"
echo "Region: $S3_REGION"
echo "Storage Type: $STORAGE_IMPL_TYPE"

if [ -n "$S3_ACCESS_KEY" ] && [ -n "$S3_SECRET_KEY" ]; then
    echo "Access Key: ******"
    echo "Secret Key: ******"

    # 简单测试
    if curl -s -f "$S3_ENDPOINT" >/dev/null 2>&1; then
        echo "✓ S3 端点可达"
    else
        echo "⚠ S3 端点暂时不可达"
    fi
else
    echo "⚠ 缺少访问凭证"
fi
EOF

chmod +x /tmp/verify-s3-config.sh

echo -e "${GREEN}配置验证脚本已保存到 /tmp/verify-s3-config.sh${NC}"

echo -e "\n${GREEN}=== 配置完成 ===${NC}"
echo -e "总结:"
echo -e "  • S3 用户: ${YELLOW}$S3_USER${NC}"
echo -e "  • S3 Bucket: ${YELLOW}$S3_BUCKET${NC}"
echo -e "  • S3 Region: ${YELLOW}$S3_REGION${NC}"
echo -e "  • S3 Endpoint: ${YELLOW}$S3_ENDPOINT${NC}"
echo -e "  • 存储类型: ${YELLOW}AWS_S3${NC}"
echo -e "\n下一步:"
echo -e "  1. 运行 ${YELLOW}pulumi preview${NC} 预览部署"
echo -e "  2. 运行 ${YELLOW}pulumi up${NC} 应用更改"
echo -e "  3. 验证 RAGFlow 是否正常使用 S3 存储"

# 清理临时 pod（如果有）
kubectl delete pod -l run=s3-test --wait=false 2>/dev/null || true
kubectl delete pod -l run=s3-bucket-create --wait=false 2>/dev/null || true