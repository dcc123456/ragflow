//go:build ee

package handler

import "ragflow/internal/server/local"

// The enterprise auth middleware refuses every authenticated request while the
// admin connection status is unavailable, and that status only becomes
// available when the server bootstrap reaches the admin service. Unit tests
// never run the bootstrap, so mark it available once for the whole test binary;
// the community stub answers true without consulting the status at all.
func init() {
	local.InitAdminStatus()
	local.SetAdminStatus(local.Available, "unit tests")
}
