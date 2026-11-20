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
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"ragflow/internal/common"
	"ragflow/internal/engine/kvrocks"
	"ragflow/internal/server"
	"ragflow/internal/service"
	"ragflow/internal/service/cas"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var casStatePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func casStateCookieName(state string) string {
	return "ragflow_cas_state_" + state
}

func setCASStateCookie(c *gin.Context, state string, maxAge int, secure bool) {
	value := state
	if maxAge < 0 {
		value = ""
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: casStateCookieName(state), Value: value, Path: "/api/v1/auth/cas/callback", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure,
	})
}

func (h *UserHandler) casLogin(c *gin.Context, channel string, asJSON bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	init, code, err := h.userService.CASLoginInitiate(c.Request.Context(), channel, kvrocks.Get())
	if err != nil {
		common.RespondError(c, code, err.Error())
		return
	}
	setCASStateCookie(c, init.State, int(init.CookieMaxAge.Seconds()), strings.HasPrefix(init.RedirectURI, "https://"))
	if asJSON {
		common.RespondSuccess(c, gin.H{
			"channel": channel, "state": init.State, "auth_url": init.AuthURL, "redirect_uri": init.RedirectURI,
		}, "success")
		return
	}
	c.Redirect(http.StatusFound, init.AuthURL)
}

// CASCallback verifies the CAS ticket and completes the existing application login flow.
func (h *UserHandler) CASCallback(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	states, tickets := c.QueryArray("state"), c.QueryArray("ticket")
	if len(states) != 1 || len(tickets) > 1 || !casStatePattern.MatchString(states[0]) {
		c.Redirect(http.StatusFound, frontendRedirectBase()+"?error=invalid_state")
		return
	}
	state := states[0]
	cookie, _ := c.Cookie(casStateCookieName(state))
	setCASStateCookie(c, state, -1, c.Request.TLS != nil)
	user, _, err := h.userService.CASCallback(c.Request.Context(), c.Query("ticket"), state, cookie, kvrocks.Get())
	if err != nil {
		common.Warn("CAS callback failed", zap.Error(err))
		message := callbackError("cas", err)
		switch {
		case errors.Is(err, service.ErrCASInvalidState):
			message = "invalid_state"
		case errors.Is(err, service.ErrCASMissingTicket):
			message = "missing_ticket"
		case errors.Is(err, cas.ErrInvalidTicket):
			message = "invalid_ticket"
		case errors.Is(err, service.ErrCASInvalidChannel):
			message = "CAS is not configured"
		}
		c.Redirect(http.StatusFound, frontendRedirectBase()+"?error="+message)
		return
	}
	secret, err := server.GetSecretKey(c.Request.Context(), kvrocks.Get())
	if err != nil {
		c.Redirect(http.StatusFound, frontendRedirectBase()+"?error=server_error")
		return
	}
	token, err := issueUserAccessToken(c.Request.Context(), user, secret)
	if err != nil {
		c.Redirect(http.StatusFound, frontendRedirectBase()+"?error=server_error")
		return
	}
	c.Header("Authorization", token)
	c.Header("Access-Control-Expose-Headers", "Authorization")
	c.Redirect(http.StatusFound, frontendRedirectBase()+"?auth="+url.QueryEscape(token))
}
