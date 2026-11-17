//go:build ee

package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"ragflow/internal/common"
)

// DeleteAccount deletes only the signed-in user's account and associated data.
func (h *UserHandler) DeleteAccount(c *gin.Context) {
	common.StartOperationLog(c, "delete_own_account")
	defer common.FinishOperationLog(c)
	user, code, message := GetUser(c)
	if code != common.CodeSuccess || user == nil || user.ID == "" {
		common.Respond(c, http.StatusUnauthorized, common.CodeUnauthorized, nil, message)
		return
	}
	if c.GetBool("auth_via_api_token") {
		common.Respond(c, http.StatusForbidden, common.CodeForbidden, nil, "Account deletion requires a user session")
		return
	}
	var request struct {
		ConfirmationEmail string `json:"confirmation_email" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.RespondError(c, common.CodeArgumentError, "Confirm account deletion by entering your account email")
		return
	}
	if _, code, err := h.userService.DeleteOwnAccount(c.Request.Context(), user.ID, request.ConfirmationEmail); err != nil {
		common.RespondError(c, code, err.Error())
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: oauthAuthCookie, Value: "", Path: "/", MaxAge: -1, SameSite: http.SameSiteLaxMode, Secure: c.Request.TLS != nil})
	common.RespondSuccess(c, nil, "Account deleted")
}
