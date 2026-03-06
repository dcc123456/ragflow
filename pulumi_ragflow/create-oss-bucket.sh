#!/bin/bash

# Script to create Aliyun OSS bucket using AWS CLI
# Usage: ./create-oss-bucket.sh

# Configuration
BUCKET_NAME="ragflow-yuzhichang-prod"
REGION="cn-shanghai"
ENDPOINT="http://oss-cn-shanghai-internal.aliyuncs.com"

echo "=========================================="
echo "Aliyun OSS Bucket Creation"
echo "=========================================="
echo "Bucket: ${BUCKET_NAME}"
echo "Region: ${REGION}"
echo "Endpoint: ${ENDPOINT}"
echo "=========================================="
echo ""

# Check if AWS CLI is installed
if ! command -v aws &> /dev/null; then
    echo "Error: AWS CLI is not installed"
    echo "Install it with: pip install awscli"
    exit 1
fi

# Check if credentials are set
if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
    echo "Error: AWS credentials not set"
    echo "Please set:"
    echo "  export AWS_ACCESS_KEY_ID=your_access_key"
    echo "  export AWS_SECRET_ACCESS_KEY=your_secret_key"
    echo ""
    echo "You can find these in Pulumi.ali.yaml (encrypted values)"
    exit 1
fi

echo "Step 1: Checking if bucket already exists..."
if aws s3 ls "s3://${BUCKET_NAME}" --region "${REGION}" --endpoint-url "${ENDPOINT}" 2>/dev/null; then
    echo "✓ Bucket '${BUCKET_NAME}' already exists and is accessible"
    echo ""
    echo "Bucket details:"
    aws s3api head-bucket --bucket "${BUCKET_NAME}" --region "${REGION}" --endpoint-url "${ENDPOINT}"
    exit 0
fi

echo "✗ Bucket does not exist or is not accessible"
echo ""
echo "Step 2: Attempting to create bucket..."

# Try to create bucket using AWS S3 mb command
if aws s3 mb "s3://${BUCKET_NAME}" --region "${REGION}" --endpoint-url "${ENDPOINT}" 2>&1 | grep -q "already exists"; then
    echo "✓ Bucket already exists (may be owned by another account)"
    echo "  If you own this bucket, verify your credentials have access"
    echo "  If you don't own this bucket, choose a different name"
    exit 1
fi

# Verify bucket was created
if aws s3 ls "s3://${BUCKET_NAME}" --region "${REGION}" --endpoint-url "${ENDPOINT}" 2>/dev/null; then
    echo "✓ Bucket '${BUCKET_NAME}' created successfully!"
    echo ""
    echo "Bucket details:"
    aws s3api head-bucket --bucket "${BUCKET_NAME}" --region "${REGION}" --endpoint-url "${ENDPOINT}"
    echo ""
    echo "=========================================="
    echo "Bucket is ready for use!"
    echo "=========================================="
    exit 0
else
    echo "✗ Failed to create bucket"
    echo ""
    echo "Possible reasons:"
    echo "  1. Bucket name is already taken (globally unique)"
    echo "  2. Insufficient permissions"
    echo "  3. Invalid credentials"
    echo ""
    echo "Solutions:"
    echo "  1. Try a different bucket name (edit BUCKET_NAME in this script)"
    echo "  2. Create bucket manually: https://oss.console.aliyun.com/"
    echo "  3. Verify your AccessKey has OSS permissions"
    exit 1
fi
