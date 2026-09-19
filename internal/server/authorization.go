package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

type accessRole uint8

const (
	roleOperator accessRole = iota + 1
	roleAdmin
)

type accessTokens struct {
	operator string
	admin    string
}

// WithAdminToken separates administrative policy changes from routine operations.
// When omitted, the operator token remains valid for both roles for compatibility.
func WithAdminToken(adminToken string) Option {
	return func(options *handlerOptions) {
		options.adminToken = adminToken
	}
}

func authorizeRole(response http.ResponseWriter, request *http.Request, tokens accessTokens, required accessRole) bool {
	adminToken := tokens.admin
	if adminToken == "" {
		adminToken = tokens.operator
	}
	if tokens.operator == "" && adminToken == "" {
		http.Error(response, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return false
	}
	authorization := request.Header.Get("Authorization")
	if validBearerToken(authorization, adminToken) {
		return true
	}
	if tokens.operator != "" && validBearerToken(authorization, tokens.operator) {
		if required == roleOperator {
			return true
		}
		http.Error(response, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return false
	}
	response.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(response, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	return false
}

func validBearerToken(authorization, expected string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return false
	}
	provided := strings.TrimPrefix(authorization, prefix)
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
