//go:build ee

package service

import (
	"errors"
	"testing"

	"ragflow/internal/dao"
	"ragflow/internal/engine"
	"ragflow/internal/entity"
	"ragflow/internal/storage"
)

func TestDeleteOwnAccount(t *testing.T) {
	factory := storage.GetStorageFactory()
	previousStorage := factory.GetStorage()
	factory.SetStorage(nil)
	t.Cleanup(func() { factory.SetStorage(previousStorage) })

	for _, tt := range []struct {
		name, confirmation          string
		memberStatus                string
		admin, member, subscription bool
		want                        error
	}{
		{name: "active user", confirmation: "actor@example.test"},
		{name: "wrong confirmation", confirmation: "other@example.test", want: errDeleteConfirmation},
		{name: "admin", confirmation: "actor@example.test", admin: true, want: errDeleteAdminAccount},
		{name: "removed member", confirmation: "actor@example.test", member: true, memberStatus: entity.TenantMembershipStatusRemoved},
		{name: "rejected invitation", confirmation: "actor@example.test", member: true, memberStatus: entity.TenantMembershipStatusRejected},
		{name: "shared workspace", confirmation: "actor@example.test", member: true, want: errDeleteSharedWorkspace},
		{name: "ongoing subscription", confirmation: "actor@example.test", subscription: true, want: errDeleteSubscription},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := setupUserDeletionDB(t)
			if err := db.AutoMigrate(&entity.BillingSubscription{}); err != nil {
				t.Fatal(err)
			}
			user := &entity.User{ID: "actor", Email: "actor@example.test", Nickname: "Actor", IsActive: "1", IsAuthenticated: "1", IsAnonymous: "0", AccessToken: new("self-delete-session-token-long-enough"), IsSuperuser: new(tt.admin)}
			if err := db.Create(user).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&entity.User{ID: "other", Email: "other@example.test", Nickname: "Other", IsActive: "1", IsAuthenticated: "1", IsAnonymous: "0"}).Error; err != nil {
				t.Fatal(err)
			}
			if tt.member || tt.subscription {
				if err := db.Exec("INSERT INTO user_tenant (id,user_id,tenant_id,role,invited_by) VALUES ('owner','actor','workspace','owner','actor')").Error; err != nil {
					t.Fatal(err)
				}
			}
			if tt.member {
				if err := db.Exec("INSERT INTO user_tenant (id,user_id,tenant_id,role,invited_by) VALUES ('member','other','workspace','normal','actor')").Error; err != nil {
					t.Fatal(err)
				}
			}
			if tt.memberStatus != "" {
				if err := db.Model(&entity.UserTenant{}).Where("id = ?", "member").Update("status", tt.memberStatus).Error; err != nil {
					t.Fatal(err)
				}
			}
			if tt.subscription {
				if err := db.Create(&entity.BillingSubscription{ID: "subscription", TenantID: "workspace", SubscriptionID: "sub_external", SubscriptionStatus: "active"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			_, _, err := NewUserService().DeleteOwnAccount(t.Context(), user.ID, tt.confirmation)
			if tt.memberStatus != "" {
				// Integration tests may have already initialized the global document engine.
				want := "document engine is unavailable for user data cleanup"
				if engine.Get() != nil {
					want = "storage is unavailable for user data cleanup"
				}
				if err == nil || err.Error() != want {
					t.Fatalf("inactive memberships blocked cleanup: %v", err)
				}
				if _, err := dao.NewUserDAO().GetByID(t.Context(), db, user.ID); err != nil {
					t.Fatalf("account removed after failed cleanup: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v want=%v", err, tt.want)
			}
			_, err = dao.NewUserDAO().GetByID(t.Context(), db, user.ID)
			if (err != nil) != (tt.want == nil) {
				t.Fatalf("remaining actor error=%v", err)
			}
			if tt.want == nil {
				if _, err := dao.NewUserDAO().GetByAccessToken(t.Context(), db, *user.AccessToken); err == nil {
					t.Fatal("deleted account still resolves through its session token")
				}
			}
			if _, err := dao.NewUserDAO().GetByID(t.Context(), db, "other"); err != nil {
				t.Fatalf("other removed: %v", err)
			}
		})
	}
}
