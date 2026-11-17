//go:build ee

package service

import (
	"context"
	"errors"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

var (
	errDeleteConfirmation    = errors.New("confirm account deletion by entering your account email")
	errDeleteAdminAccount    = errors.New("admin accounts cannot be deleted")
	errDeleteSharedWorkspace = errors.New("transfer workspace ownership or remove other members before deleting your account")
	errDeleteSubscription    = errors.New("cancel your workspace subscription and wait until it ends before deleting your account")
)

// DeleteOwnAccount permits an active user to delete only their authenticated account.
func (s *UserService) DeleteOwnAccount(ctx context.Context, userID, confirmation string) (*DeleteUserResult, common.ErrorCode, error) {
	user, err := s.userDAO.GetByID(ctx, dao.DB, userID)
	if err != nil {
		return nil, common.CodeUnauthorized, err
	}
	if user.Email == "" || confirmation != user.Email {
		return nil, common.CodeArgumentError, errDeleteConfirmation
	}
	if user.IsSuperuser != nil && *user.IsSuperuser {
		return nil, common.CodeForbidden, errDeleteAdminAccount
	}
	owned := dao.DB.WithContext(ctx).Model(&entity.UserTenant{}).Select("tenant_id").Where("user_id = ? AND role = ?", userID, "owner")
	var members int64
	if err := dao.DB.WithContext(ctx).Model(&entity.UserTenant{}).Where("tenant_id IN (?) AND user_id <> ? AND (status IS NULL OR status IN ?)", owned, userID, []string{entity.TenantMembershipStatusJoined, entity.TenantMembershipStatusInvited, entity.TenantMembershipStatusRequested}).Count(&members).Error; err != nil {
		return nil, common.CodeServerError, err
	}
	if members > 0 {
		return nil, common.CodeForbidden, errDeleteSharedWorkspace
	}
	// A scheduled cancellation can still renew until the provider confirms its terminal state.
	var subscriptions int64
	if err := dao.DB.WithContext(ctx).Model(&entity.BillingSubscription{}).Where("tenant_id IN (?) AND subscription_id <> ? AND subscription_status NOT IN ?", owned, "", []string{"canceled", "incomplete_expired"}).Count(&subscriptions).Error; err != nil {
		return nil, common.CodeServerError, err
	}
	if subscriptions > 0 {
		return nil, common.CodeForbidden, errDeleteSubscription
	}
	result, err := (UserDeletionService{}).Delete(ctx, user)
	if err != nil {
		return nil, common.CodeServerError, err
	}
	return result, common.CodeSuccess, nil
}
