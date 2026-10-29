//go:build ee

package testsupport

import "gorm.io/gorm"

func PrepareEnterprisePermissionFixture(_ *gorm.DB, _ string, _ ...string) error {
	return nil
}
