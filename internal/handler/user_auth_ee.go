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

//go:build ee

package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"ragflow/internal/common"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/entity"
	"ragflow/internal/server"
	"ragflow/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// isOAuthLoginNoRoute reports the OAuth login URL with an empty channel.
func isOAuthLoginNoRoute(c *gin.Context) bool {
	return c.Request.Method == http.MethodGet && c.Request.URL.Path == "/api/v1/auth/login/"
}

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

	setOAuthStateCookie(c, channel, init.State, int(init.CookieMaxAge.Seconds()))
	c.Redirect(http.StatusFound, init.AuthURL)
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

	setOAuthStateCookie(c, channel, init.State, int(init.CookieMaxAge.Seconds()))
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
	cookieState := readOAuthStateCookie(c, channel)
	// Clear before any response is written: the state is single-use, and a
	// cookie deleted after the redirect headers are buffered would survive.
	clearOAuthStateCookie(c, channel)

	frontendBase := frontendRedirectBase()

	result, _, err := h.userService.OAuthCallback(c.Request.Context(), channel, queryCode, queryState, cookieState, kvrocks.Get())
	if err != nil {
		common.Error("oauth callback failed", err, zap.String("channel", channel))
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

	//setOAuthAuthCookie(c, authToken)
	c.Header("Authorization", authToken)
	c.Header("Access-Control-Expose-Headers", "Authorization")
	// The SPA stores this value verbatim and replays it as the Authorization
	// header, so it has to be the signed access token. Handing back the user
	// ID instead produces an unparsable credential: the token format check
	// rejects it, every authenticated call answers 401 and the frontend
	// clears its storage and bounces to the login page.
	c.Redirect(http.StatusFound, frontendBase+"?auth="+url.QueryEscape(authToken))
}

func issueUserAccessToken(user *entity.User, secretKey string) (string, error) {
	if user == nil || user.AccessToken == nil {
		return "", fmt.Errorf("user access token is missing")
	}
	// Users enter their own Tenant after login; the EE token keeps that context.
	return common.DumpAccessToken(*user.AccessToken, user.ID, secretKey)
}

// oauthStateCookiePrefix namespaces the per-channel HttpOnly cookie that ties
// the in-flight state token to the browser that initiated the flow. The
// handler reads it back from the callback request to defend against CSRF in
// addition to the store-side verification. The channel is part of the name so
// two logins started in parallel (different tabs/channels) cannot overwrite
// each other's state.
const oauthStateCookiePrefix = "ragflow_oauth_state_"

func oauthStateCookieName(channel string) string {
	return oauthStateCookiePrefix + channel
}

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
	case errors.Is(err, service.ErrOAuthEmailUnverified):
		return "email_unverified"
	default:
		return "server_error"
	}
}

// setOAuthStateCookie writes the state token as an HttpOnly cookie scoped
// to the API host. SameSite=Lax keeps the cookie attached on the top-level
// navigation that brings the user back to the callback. HttpOnly because the
// SPA never needs to read it — it only has to survive the round trip to the
// provider.
func setOAuthStateCookie(c *gin.Context, channel, state string, maxAgeSec int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthStateCookieName(channel),
		Value:    state,
		Path:     "/",
		MaxAge:   maxAgeSec,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.Request.TLS != nil,
	})
}

func readOAuthStateCookie(c *gin.Context, channel string) string {
	if cookie, err := c.Request.Cookie(oauthStateCookieName(channel)); err == nil {
		return cookie.Value
	}
	return ""
}

func clearOAuthStateCookie(c *gin.Context, channel string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthStateCookieName(channel),
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

type forgotCaptchaRequest struct {
	Email string `form:"email" json:"email"`
}

// ForgotCaptcha POST /api/v1/auth/password/forgot/captcha
// @Summary Issue forgot-password captcha
// @Description Generates a captcha for the email and stores it in Redis
// for 60 seconds keyed by a server-issued captcha_id. Returns the id
// and a PNG image (data URL) the FE renders inside <img src>. The
// plaintext code never appears in the response — only as raster
// pixels — so a scripted client can't regex it out (fixes the
// SVG-text leak from the previous iteration, per PR #15290 review).
// @Tags auth
// @Param email query string false "user email (also accepted in JSON body)"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/auth/password/forgot/captcha [post]
func (h *UserHandler) ForgotCaptcha(c *gin.Context) {
	var req forgotCaptchaRequest
	// Python reads from request.args (query string), accept both for parity.
	if v := c.Query("email"); v != "" {
		req.Email = v
	} else {
		_ = c.ShouldBindJSON(&req)
	}

	ctx := c.Request.Context()
	captchaID, captchaImage, errCode, err := h.userService.ForgotIssueCaptcha(ctx, req.Email)
	if err != nil {
		common.ResponseWithCodeData(c, errCode, false, err.Error())
		return
	}
	common.SuccessWithData(c, gin.H{
		"captcha_id":    captchaID,
		"captcha_image": captchaImage,
	}, "captcha issued")
}

type forgotSendOTPRequest struct {
	Email     string `json:"email"`
	CaptchaID string `json:"captcha_id"`
	Captcha   string `json:"captcha"`
}

// ForgotSendOTP POST /api/v1/auth/password/forgot/otp
// @Summary Send forgot-password OTP
// @Description Validates the captcha (looked up by captcha_id), then
// mints a one-time code, stores a salted hash in Redis (5 min TTL,
// attempt cap, resend cooldown), and emails the OTP to the user.
// @Param request body forgotSendOTPRequest true "email + captcha_id + captcha"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/auth/password/forgot/otp [post]
func (h *UserHandler) ForgotSendOTP(c *gin.Context) {
	var req forgotSendOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ResponseWithCodeData(c, common.CodeArgumentError, false, err.Error())
		return
	}
	ctx := c.Request.Context()
	errCode, err := h.userService.ForgotSendOTP(ctx, req.Email, req.CaptchaID, req.Captcha)
	if err != nil {
		common.ResponseWithCodeData(c, errCode, false, err.Error())
		return
	}
	common.SuccessWithData(c, true, "verification passed, email sent")
}

type forgotVerifyOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

// ForgotVerifyOTP POST /api/v1/auth/password/forgot/otp/verify
// @Summary Verify forgot-password OTP
// @Description Consumes the OTP if it matches, sets a short-lived
// verified flag the reset endpoint will gate on. Wrong-OTP attempts
// are counted and a 30-minute lockout kicks in at the limit.
// @Tags auth
// @Param request body forgotVerifyOTPRequest true "email + otp"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/auth/password/forgot/otp/verify [post]
func (h *UserHandler) ForgotVerifyOTP(c *gin.Context) {
	var req forgotVerifyOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ResponseWithCodeData(c, common.CodeArgumentError, false, err.Error())
		return
	}
	ctx := c.Request.Context()
	errCode, err := h.userService.ForgotVerifyOTP(ctx, req.Email, req.OTP)
	if err != nil {
		common.ResponseWithCodeData(c, errCode, false, err.Error())
		return
	}
	common.SuccessWithData(c, true, "otp verified")
}

// ForgotResetPassword POST /api/v1/auth/password/reset
// @Summary Reset password after OTP verification
// @Description Requires a successful prior verify call (verified flag
// set in Redis). Updates the password hash and rotates the access
// token so the response can auto-login the user.
// @Tags auth
// @Param request body service.ForgotResetPasswordRequest true "email + new password"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/auth/password/reset [post]
func (h *UserHandler) ForgotResetPassword(c *gin.Context) {
	ctx := c.Request.Context()
	var req service.ForgotResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ResponseWithCodeData(c, common.CodeArgumentError, false, err.Error())
		return
	}

	user, code, err := h.userService.ForgotResetPassword(ctx, &req)
	if err != nil {
		common.ResponseWithCodeData(c, code, false, err.Error())
		return
	}

	secretKey, err := server.GetSecretKey(ctx, kvrocks.Get())
	if err != nil {
		common.ResponseWithCodeData(c, common.CodeServerError, false, fmt.Sprintf("Failed to get secret key: %s", err.Error()))
		return
	}
	authToken, err := issueUserAccessToken(user, secretKey)
	if err != nil {
		common.ResponseWithCodeData(c, common.CodeServerError, false, "Failed to generate auth token")
		return
	}
	c.Header("Authorization", authToken)
	c.Header("Access-Control-Expose-Headers", "Authorization")

	profile := h.userService.GetUserProfile(ctx, user)
	common.SuccessWithData(c, profile, "Password reset successful. Logged in.")
}
