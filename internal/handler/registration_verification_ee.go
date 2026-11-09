//go:build ee

package handler

import (
	"net/http"
	"strings"

	"ragflow/internal/service"
	"ragflow/internal/utility"

	"github.com/gin-gonic/gin"
)

const registrationProofCookie = "ragflow_registration"

// RegistrationVerification passes the browser's proof to the registration service.
func RegistrationVerification() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost && c.FullPath() == "/api/v1/users" {
			proof, _ := c.Cookie(registrationProofCookie)
			c.Request = c.Request.WithContext(service.WithRegistrationProof(c.Request.Context(), proof))
		}
		c.Next()
	}
}

func setRegistrationProofCookie(c *gin.Context, proof string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     registrationProofCookie,
		Value:    proof,
		Path:     "/api/v1/users",
		MaxAge:   int(utility.OTPTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https"),
	})
}
