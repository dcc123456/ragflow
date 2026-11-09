//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
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
		common.RespondError(c, code, "Load channels failure, error: "+err.Error())
		return
	}

	common.RespondSuccess(c, channels, "success")
}

func (h *UserHandler) OAuthCallback(c *gin.Context) {
	channel := c.Param("channel")
	if channel == "" {
		common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "channel is required")
		return
	}

	init, code, err := h.userService.OAuthLoginInitiate(c.Request.Context(), channel, kvrocks.Get())
	if err != nil {
		// Mirror Python's oauth_login: the raised ValueError propagates to
		// server_error_response, which replies HTTP 200 with code 100 and
		// the exception's repr() as the message (no short error code).
		if errors.Is(err, service.ErrOAuthInvalidChannel) {
			common.RespondError(c, common.CodeExceptionError, fmt.Sprintf("ValueError('Invalid channel name: %s')", channel))
			return
		}
		common.Respond(c, http.StatusInternalServerError, code, nil, err.Error())
		return
	}

	setOAuthStateCookie(c, channel, init.State, int(init.CookieMaxAge.Seconds()))
	c.Redirect(http.StatusFound, init.AuthURL)
}

func (h *UserHandler) Captcha(c *gin.Context) {
	var request struct {
		Email string `json:"email"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if c.ShouldBindJSON(&request) != nil {
		respondEmailVerification(c, common.CodeArgumentError, "invalid request")
		return
	}
	image, err := h.userService.IssueRegistrationCaptcha(c.Request.Context(), request.Email)
	if err != nil {
		respondRegistrationVerificationError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "image/png", image)
}

func (h *UserHandler) SendOTP(c *gin.Context) {
	var request struct {
		Email   string `json:"email"`
		Captcha string `json:"captcha"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if c.ShouldBindJSON(&request) != nil {
		respondEmailVerification(c, common.CodeArgumentError, "invalid request")
		return
	}
	if err := h.userService.SendRegistrationOTP(c.Request.Context(), request.Email, request.Captcha); err != nil {
		respondRegistrationVerificationError(c, err)
		return
	}
	respondEmailVerification(c, common.CodeSuccess, "verification passed, email sent")
}

func (h *UserHandler) VerifyOTP(c *gin.Context) {
	var request struct {
		Email string `json:"email"`
		OTP   string `json:"otp"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if c.ShouldBindJSON(&request) != nil {
		respondEmailVerification(c, common.CodeArgumentError, "invalid request")
		return
	}
	proof, err := h.userService.VerifyRegistrationOTP(c.Request.Context(), request.Email, request.OTP)
	if err != nil {
		respondRegistrationVerificationError(c, err)
		return
	}
	setRegistrationProofCookie(c, proof)
	respondEmailVerification(c, common.CodeSuccess, "otp verified")
}

func respondEmailVerification(c *gin.Context, code common.ErrorCode, message string) {
	c.Header("Content-Type", "application/json")
	c.JSON(http.StatusOK, gin.H{"code": code, "data": code == common.CodeSuccess, "message": message})
}

func respondRegistrationVerificationError(c *gin.Context, err error) {
	var coded *common.CodedError
	if errors.As(err, &coded) {
		respondEmailVerification(c, coded.Code, coded.Message)
		return
	}
	common.Error("Registration verification failed", err)
	respondEmailVerification(c, common.CodeServerError, "email verification is temporarily unavailable")
}

// IsAdmin reports whether the caller holds the superuser flag. The web client
// reads data.admin to decide whether to expose the model-provider admin pages.
func (h *UserHandler) IsAdmin(c *gin.Context) {
	user, code, message := GetUser(c)
	if code != common.CodeSuccess {
		common.RespondError(c, code, message)
		return
	}
	admin := user.IsSuperuser != nil && *user.IsSuperuser
	common.RespondSuccess(c, gin.H{"admin": admin}, "")
}

func (h *UserHandler) GetMeta(c *gin.Context) {
	common.RespondError(c, common.CodeNotImplemented, "GetMeta not implemented")
}

func (h *UserHandler) OAuthLogin(c *gin.Context) {
	channel := c.Param("channel")
	if channel == "" {
		common.Respond(c, http.StatusBadRequest, common.CodeArgumentError, nil, "channel is required")
		return
	}

	// A directory channel shares this route with the OAuth providers because
	// the login page posts a channel name either way, but it authenticates
	// inline instead of redirecting to a provider.
	if service.IsLDAPChannel(channel) {
		h.ldapLogin(c, channel)
		return
	}

	init, code, err := h.userService.OAuthLoginInitiate(c.Request.Context(), channel, kvrocks.Get())
	if err != nil {
		// Mirror Python's oauth_login: the raised ValueError propagates to
		// server_error_response, which replies HTTP 200 with code 100 and
		// the exception's repr() as the message (no short error code).
		if errors.Is(err, service.ErrOAuthInvalidChannel) {
			common.RespondError(c, common.CodeExceptionError, fmt.Sprintf("ValueError('Invalid channel name: %s')", channel))
			return
		}
		common.Respond(c, http.StatusInternalServerError, code, nil, err.Error())
		return
	}

	setOAuthStateCookie(c, channel, init.State, int(init.CookieMaxAge.Seconds()))
	c.Redirect(http.StatusFound, init.AuthURL)
}

// ldapLogin serves the directory branch of GET /auth/login/:channel. The login
// form sends the credentials as query parameters and expects the same `?auth=`
// redirect the OAuth callback produces, because the SPA reads the token off the
// redirect target. Failures answer with a JSON envelope instead, which is what
// the form turns into an error toast.
func (h *UserHandler) ldapLogin(c *gin.Context, channel string) {
	user, code, err := h.userService.LDAPLogin(c.Request.Context(), channel, c.Query("username"), c.Query("password"))
	if err != nil {
		common.RespondError(c, code, err.Error())
		return
	}

	frontendBase := frontendRedirectBase()
	secretKey, kerr := server.GetSecretKey(c.Request.Context(), kvrocks.Get())
	if kerr != nil {
		c.Redirect(http.StatusFound, frontendBase+"?error=server_error")
		return
	}
	authToken, terr := issueUserAccessToken(user, secretKey)
	if terr != nil {
		c.Redirect(http.StatusFound, frontendBase+"?error=server_error")
		return
	}

	c.Header("Authorization", authToken)
	c.Header("Access-Control-Expose-Headers", "Authorization")
	c.Redirect(http.StatusFound, frontendBase+"?auth="+url.QueryEscape(authToken))
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
	Email string `json:"email"`
}

// ForgotCaptcha POST /api/v1/auth/password/forgot/captcha
// @Summary Issue forgot-password captcha
// @Description Returns a PNG captcha bound to the email for 60 seconds.
// @Tags auth
// @Accept json
// @Produce png
// @Param request body forgotCaptchaRequest true "email"
// @Success 200 {file} binary
// @Router /api/v1/auth/password/forgot/captcha [post]
func (h *UserHandler) ForgotCaptcha(c *gin.Context) {
	var req forgotCaptchaRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if c.ShouldBindJSON(&req) != nil {
		respondEmailVerification(c, common.CodeArgumentError, "invalid request")
		return
	}

	ctx := c.Request.Context()
	image, errCode, err := h.userService.ForgotIssueCaptcha(ctx, req.Email)
	if err != nil {
		respondEmailVerification(c, errCode, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "image/png", image)
}

type forgotSendOTPRequest struct {
	Email   string `json:"email"`
	Captcha string `json:"captcha"`
}

// ForgotSendOTP POST /api/v1/auth/password/forgot/otp
// @Summary Send forgot-password OTP
// @Description Validates the captcha bound to the email, then
// mints a one-time code, stores a salted hash in Redis (5 min TTL,
// attempt cap, resend cooldown), and emails the OTP to the user.
// @Param request body forgotSendOTPRequest true "email + captcha"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/auth/password/forgot/otp [post]
func (h *UserHandler) ForgotSendOTP(c *gin.Context) {
	var req forgotSendOTPRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := c.ShouldBindJSON(&req); err != nil {
		respondEmailVerification(c, common.CodeArgumentError, "invalid request")
		return
	}
	ctx := c.Request.Context()
	errCode, err := h.userService.ForgotSendOTP(ctx, req.Email, req.Captcha)
	if err != nil {
		respondEmailVerification(c, errCode, err.Error())
		return
	}
	respondEmailVerification(c, common.CodeSuccess, "verification passed, email sent")
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
		respondEmailVerification(c, common.CodeArgumentError, err.Error())
		return
	}
	ctx := c.Request.Context()
	errCode, err := h.userService.ForgotVerifyOTP(ctx, req.Email, req.OTP)
	if err != nil {
		respondEmailVerification(c, errCode, err.Error())
		return
	}
	respondEmailVerification(c, common.CodeSuccess, "otp verified")
}

// ForgotResetPassword POST /api/v1/auth/password/reset
// @Summary Reset password after OTP verification
// @Description Updates the password and logs in after successful email verification.
// @Tags auth
// @Param request body service.ForgotResetPasswordRequest true "email and new passwords"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/auth/password/reset [post]
func (h *UserHandler) ForgotResetPassword(c *gin.Context) {
	ctx := c.Request.Context()
	var req service.ForgotResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondEmailVerification(c, common.CodeArgumentError, err.Error())
		return
	}

	user, code, err := h.userService.ForgotResetPassword(ctx, &req)
	if err != nil {
		respondEmailVerification(c, code, err.Error())
		return
	}

	secretKey, err := server.GetSecretKey(ctx, kvrocks.Get())
	if err != nil {
		respondEmailVerification(c, common.CodeServerError, fmt.Sprintf("Failed to get secret key: %s", err.Error()))
		return
	}
	authToken, err := issueUserAccessToken(user, secretKey)
	if err != nil {
		respondEmailVerification(c, common.CodeServerError, "Failed to generate auth token")
		return
	}
	c.Header("Authorization", authToken)
	c.Header("Access-Control-Expose-Headers", "Authorization")

	profile := h.userService.GetUserProfile(ctx, user)
	common.RespondSuccess(c, profile, "Password reset successful. Logged in.")
}
