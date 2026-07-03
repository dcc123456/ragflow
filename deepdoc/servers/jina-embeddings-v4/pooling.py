#!/usr/bin/env python3
"""Refers to:
- https://github.com/lightonai/pylate-rs/blob/main/src/pooling.rs
- https://arxiv.org/abs/2409.14683
"""

import numpy as np
from scipy.cluster.hierarchy import fcluster, linkage


class Dsu:
    """Disjoint Set Union (Union-Find) implementation for cluster label merging."""

    def __init__(self):
        self.parent = dict()

    def find(self, x):
        if x not in self.parent:
            self.parent[x] = x
        if self.parent[x] != x:
            self.parent[x] = self.find(self.parent[x])
        return self.parent[x]

    def union(self, x, y):
        x_root = self.find(x)
        y_root = self.find(y)
        if x_root != y_root:
            self.parent[y_root] = x_root


def hierarchical_pooling(documents_embeddings: np.ndarray, pool_factor: int) -> list[np.ndarray]:
    """documents_embeddings: shape [batch_size, n_tokens, embedding_dim]
    pool_factor: int, number of tokens per cluster
    Returns: list, each element is a pooled document embedding, shape [new_n_tokens, embedding_dim]
    """
    if pool_factor <= 1:
        return [doc.copy() for doc in documents_embeddings]
    if documents_embeddings.ndim != 3:
        raise ValueError(f"Input tensor must have 3 dimensions [batch_size, n_tokens, embedding_dim], but got {documents_embeddings.ndim} dimensions.")

    batch_size, n_tokens, embedding_dim = documents_embeddings.shape
    all_pooled_embeddings = []

    for i in range(batch_size):
        document_embeddings = documents_embeddings[i]  # [n_tokens, embedding_dim]
        if n_tokens <= 1:
            all_pooled_embeddings.append(document_embeddings.copy())
            continue

        protected_embeddings = document_embeddings[0:1]  # [1, embedding_dim], usually the first token (e.g., [CLS])
        embeddings_to_pool = document_embeddings[1:]  # [n_tokens-1, embedding_dim], tokens to be pooled
        num_embeddings_to_pool = embeddings_to_pool.shape[0]

        if num_embeddings_to_pool <= 1:
            final_embeddings = np.concatenate([protected_embeddings, embeddings_to_pool], axis=0)
            all_pooled_embeddings.append(final_embeddings)
            continue

        # Normalize embeddings
        normed = embeddings_to_pool / (np.linalg.norm(embeddings_to_pool, axis=1, keepdims=True) + 1e-8)
        cosine_similarities = np.matmul(normed, normed.T)
        distance_matrix = 1.0 - cosine_similarities

        # Condense the upper triangle of the distance matrix for scipy linkage
        condensed_distances = distance_matrix[np.triu_indices(num_embeddings_to_pool, k=1)]

        # Hierarchical clustering
        Z = linkage(condensed_distances, method="ward")
        num_clusters = max(num_embeddings_to_pool // pool_factor, 1)

        if num_clusters >= num_embeddings_to_pool:
            final_embeddings = np.concatenate([protected_embeddings, embeddings_to_pool], axis=0)
            all_pooled_embeddings.append(final_embeddings)
            continue

        # Cluster labels
        labels = fcluster(Z, num_clusters, criterion="maxclust") - 1  # 0-based

        pooled_document_embeddings = []
        for cluster_id in range(num_clusters):
            cluster_indices = np.where(labels == cluster_id)[0]
            if len(cluster_indices) > 0:
                cluster_embeddings = embeddings_to_pool[cluster_indices]
                pooled_document_embeddings.append(cluster_embeddings.mean(axis=0, keepdims=True))
        # Keep protected_embeddings at the beginning
        final_embeddings_list = [protected_embeddings] + pooled_document_embeddings
        final_doc_tensor = np.concatenate(final_embeddings_list, axis=0)
        all_pooled_embeddings.append(final_doc_tensor)

    return all_pooled_embeddings


def main():
    # Example: batch_size=2, n_tokens=5, embedding_dim=3
    np.random.seed(42)
    batch = np.random.randn(2, 5, 3).astype(np.float32)
    pool_factor = 2
    pooled = hierarchical_pooling(batch, pool_factor)
    for i, doc in enumerate(pooled):
        print(f"Doc {i} pooled shape: {doc.shape}\n{doc}\n")


def test_hierarchical_pooling():
    np.random.seed(0)
    batch = np.random.randn(1, 6, 4).astype(np.float32)
    pool_factor = 2
    pooled = hierarchical_pooling(batch, pool_factor)
    assert len(pooled) == 1
    pooled_doc = pooled[0]
    # protected_embeddings + ceil((n_tokens-1)/pool_factor)
    expected_clusters = max((batch.shape[1] - 1) // pool_factor, 1)
    assert pooled_doc.shape[0] == 1 + expected_clusters or pooled_doc.shape[0] == batch.shape[1]
    assert pooled_doc.shape[1] == 4
    print("Unit test passed.")


if __name__ == "__main__":
    main()
    test_hierarchical_pooling()
