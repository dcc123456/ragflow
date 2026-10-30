//
// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

//go:build ee

package document

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func grantDocumentOwnerEE(ctx context.Context, tx *gorm.DB, doc *entity.Document) error {
	if doc == nil || strings.TrimSpace(doc.CreatedBy) == "" {
		return fmt.Errorf("document owner is required")
	}
	var dataset entity.Knowledgebase
	if err := tx.WithContext(ctx).Select("tenant_id").Where("id = ?", doc.KbID).Take(&dataset).Error; err != nil {
		return fmt.Errorf("load document dataset Tenant: %w", err)
	}
	checker := permission.NewDatabaseChecker(tx)
	return checker.GrantResourceOwner(ctx,
		permission.Subject{UserID: doc.CreatedBy, TenantID: dataset.TenantID},
		permission.ResourceRef{Kind: permission.ResourceKindDocument, ID: doc.ID},
	)
}
