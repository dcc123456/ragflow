#!/bin/bash
set -e

echo "Verifying ECK Elasticsearch deployment..."

# Check if ECK operator is running
echo "1. Checking ECK Operator..."
kubectl get pods -n elastic-system -l control-plane=elastic-operator

# Check if Elasticsearch custom resource exists
echo -e "\n2. Checking Elasticsearch custom resource..."
kubectl get elasticsearch -A

# Check Elasticsearch pods
echo -e "\n3. Checking Elasticsearch pods..."
kubectl get pods -l elasticsearch.k8s.elastic.co/cluster-name=elasticsearch

# Check Elasticsearch services
echo -e "\n4. Checking Elasticsearch services..."
kubectl get svc -l elasticsearch.k8s.elastic.co/cluster-name=elasticsearch

# Check if password secret exists
echo -e "\n5. Checking password secret..."
kubectl get secret elasticsearch-password

# Test Elasticsearch connection (assuming kubectl proxy or port-forward)
echo -e "\n6. Testing Elasticsearch connection..."
echo "Note: You may need to set up port-forward first:"
echo "  kubectl port-forward service/elasticsearch-es-http 9200:9200"
echo "Then test with:"
echo "  curl -k -u elastic:infini_rag_flow https://localhost:9200"

# Check RAGFlow environment variables
echo -e "\n7. Checking RAGFlow pod environment..."
RAGFLOW_POD=$(kubectl get pods -l app=ragflow -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$RAGFLOW_POD" ]; then
    echo "RAGFlow pod found: $RAGFLOW_POD"
    echo "Checking ES environment variable:"
    kubectl exec $RAGFLOW_POD -- printenv ES 2>/dev/null || echo "ES env var not found"
else
    echo "RAGFlow pod not found yet"
fi

echo -e "\nVerification completed."