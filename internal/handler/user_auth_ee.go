//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package handler

import (
	"errors"
	"fmt"
	"net/http"
	"ragflow/internal/common"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/entity"
	"ragflow/internal/server"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
)

// GetLoginChannels get all supported authentication channels
// @Summary Get Login Channels
// @Description Get all supported OAuth authentication channels
// @Tags users
// @Success 200 {object} map[string]interface{}
// @Router /v1/user/login/channels [get]
func (h *UserHandler) GetLoginChannels(c *gin.Context) {
	channels, code, err := h.userService.GetLoginChannels()
	if err != nil {
		common.ResponseWithCodeData(c, code, []interface{}{}, "Load channels failure, error: "+err.Error())
		return
	}

	common.SuccessWithData(c, channels, "success")
}

func (h *UserHandler) OAuthCallback(c *gin.Context) {
	channel := c.Param("channel")
	if channel == "" {
		common.ResponseWithHttpCodeData(c, http.StatusBadRequest, common.CodeArgumentError, nil, "channel is required")
		return
	}

	init, code, err := h.userService.OAuthLoginInitiate(c.Request.Context(), channel, kvrocks.Get())
	if err != nil {
		// Mirror Python's oauth_login: the raised ValueError propagates to
		// server_error_response, which replies HTTP 200 with code 100 and
		// the exception's repr() as the message (no short error code).
		if errors.Is(err, service.ErrOAuthInvalidChannel) {
			common.ResponseWithCodeData(c, common.CodeExceptionError, nil, fmt.Sprintf("ValueError('Invalid channel name: %s')", channel))
			return
		}
		common.ResponseWithHttpCodeData(c, http.StatusInternalServerError, code, nil, err.Error())
		return
	}

	setOAuthStateCookie(c, init.State, int(init.CookieMaxAge.Seconds()))
	c.Redirect(http.StatusFound, init.AuthURL)
}

func (h *UserHandler) GitHubAuthCallback(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "GitHubAuthCallback not implemented")
}

func (h *UserHandler) LarkAuthCallback(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "LarkAuthCallback not implemented")
}

func (h *UserHandler) ICBCAuthCallback(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "ICBCAuthCallback not implemented")
}

func (h *UserHandler) AzureAuthCallback(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "AzureAuthCallback not implemented")
}

func (h *UserHandler) AzureAuthLogin(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "AzureAuthLogin not implemented")
}

func (h *UserHandler) Captcha(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "Captcha not implemented")
}

func (h *UserHandler) SendOTP(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "SendOTP not implemented")
}

func (h *UserHandler) VerifyOTP(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "VerifyOTP not implemented")
}

func (h *UserHandler) IsAdmin(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "IsAdmin not implemented")
}

func (h *UserHandler) GetMeta(c *gin.Context) {
	common.ErrorWithCode(c, common.CodeNotImplemented, "GetMeta not implemented")
}

func (h *UserHandler) OAuthLogin(c *gin.Context) {
	channel := c.Param("channel")
	if channel == "" {
		common.ResponseWithHttpCodeData(c, http.StatusBadRequest, common.CodeArgumentError, nil, "channel is required")
		return
	}

	init, code, err := h.userService.OAuthLoginInitiate(c.Request.Context(), channel, kvrocks.Get())
	if err != nil {
		// Mirror Python's oauth_login: the raised ValueError propagates to
		// server_error_response, which replies HTTP 200 with code 100 and
		// the exception's repr() as the message (no short error code).
		if errors.Is(err, service.ErrOAuthInvalidChannel) {
			common.ResponseWithCodeData(c, common.CodeExceptionError, nil, fmt.Sprintf("ValueError('Invalid channel name: %s')", channel))
			return
		}
		common.ResponseWithHttpCodeData(c, http.StatusInternalServerError, code, nil, err.Error())
		return
	}

	setOAuthStateCookie(c, init.State, int(init.CookieMaxAge.Seconds()))
	c.Redirect(http.StatusFound, init.AuthURL)
}

func (h *UserHandler) OAuthChannelCallback(c *gin.Context) {
	channel := c.Param("channel")
	// An empty channel segment (/auth/oauth//callback) is a malformed path,
	// not a real channel. Python's router never matches it and returns 404;
	// match that here instead of flowing into the callback and emitting a
	// bogus "Invalid channel name:" redirect.
	if channel == "" {
		HandleNoRoute(c)
		return
	}
	queryCode := c.Query("code")
	queryState := c.Query("state")
	cookieState := readOAuthStateCookie(c)
	clearOAuthStateCookie(c)

	frontendBase := frontendRedirectBase()

	result, _, err := h.userService.OAuthCallback(c.Request.Context(), channel, queryCode, queryState, cookieState, kvrocks.Get())
	if err != nil {
		c.Redirect(http.StatusFound, frontendBase+"?error="+callbackError(channel, err))
		return
	}

	secretKey, kerr := server.GetSecretKey(c.Request.Context(), kvrocks.Get())
	if kerr != nil {
		c.Redirect(http.StatusFound, frontendBase+"?error=server_error")
		return
	}
	authToken, terr := issueUserAccessToken(result.User, secretKey)
	if terr != nil {
		c.Redirect(http.StatusFound, frontendBase+"?error=server_error")
		return
	}

	setOAuthAuthCookie(c, authToken)
	c.Header("Authorization", authToken)
	c.Header("Access-Control-Expose-Headers", "Authorization")
	c.Redirect(http.StatusFound, frontendBase+"?auth="+result.User.ID)
}

func issueUserAccessToken(user *entity.User, secretKey string) (string, error) {
	if user == nil || user.AccessToken == nil {
		return "", fmt.Errorf("user access token is missing")
	}
	return common.DumpAccessToken(*user.AccessToken, "", secretKey)
}

// LoginChannel represents a login channel response
type LoginChannel struct {
	Channel     string `json:"channel"`
	DisplayName string `json:"display_name"`
	Icon        string `json:"icon"`
}

// oauthStateCookie is the HttpOnly cookie name that ties the in-flight
// state token to the browser that initiated the flow. The handler reads
// it back from the callback request to defend against CSRF in addition to
// the Redis-side verification.
const oauthStateCookie = "ragflow_oauth_state"

// callbackError maps the OAuth callback errors to the `?error=` strings
// Python's oauth_callback emits. Python redirects with `?error={str(e)}`,
// so an invalid channel surfaces the full "Invalid channel name: <channel>"
// message (str of the ValueError), while the other failures use the short
// tokens Python hard-codes. The value is intentionally not URL-encoded to
// match Python's raw f-string redirect.
func callbackError(channel string, err error) string {
	switch {
	case errors.Is(err, service.ErrOAuthInvalidChannel):
		return "Invalid channel name: " + channel
	case errors.Is(err, service.ErrOAuthInvalidState):
		return "invalid_state"
	case errors.Is(err, service.ErrOAuthMissingCode):
		return "missing_code"
	case errors.Is(err, service.ErrOAuthTokenFailed):
		return "token_failed"
	case errors.Is(err, service.ErrOAuthEmailMissing):
		return "email_missing"
	case errors.Is(err, service.ErrOAuthUserInactive):
		return "user_inactive"
	default:
		return "server_error"
	}
}

// setOAuthStateCookie writes the state token as an HttpOnly cookie scoped
// to the API host. SameSite=Lax keeps the cookie attached on the top-level
// navigation that brings the user back to the callback.
func setOAuthStateCookie(c *gin.Context, state string, maxAgeSec int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   maxAgeSec,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.Request.TLS != nil,
	})
}

func readOAuthStateCookie(c *gin.Context) string {
	if cookie, err := c.Request.Cookie(oauthStateCookie); err == nil {
		return cookie.Value
	}
	return ""
}

func clearOAuthStateCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.Request.TLS != nil,
	})
}

// frontendRedirectBase returns the URL prefix the OAuth callback should
// redirect back to. Mirrors Python's oauth_callback, which always issues
// relative "/?auth=..." / "/?error=..." redirects so the browser stays on
// the same origin that served the SPA.
func frontendRedirectBase() string {
	return "/"
}
