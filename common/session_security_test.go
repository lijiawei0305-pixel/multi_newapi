package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSessionSecurity(t *testing.T) {
	tests := []struct {
		name          string
		production    bool
		cookieSecure  bool
		sessionSecret string
		cryptoSecret  string
		wantError     bool
	}{
		{name: "local explicit http remains supported", wantError: false},
		{name: "production secure and split secrets", production: true, cookieSecure: true, sessionSecret: "session-secret-with-at-least-32-bytes", cryptoSecret: "crypto-secret-with-at-least-32-bytes", wantError: false},
		{name: "production insecure cookie", production: true, sessionSecret: "session-secret-with-at-least-32-bytes", cryptoSecret: "crypto-secret-with-at-least-32-bytes", wantError: true},
		{name: "production missing session secret", production: true, cookieSecure: true, cryptoSecret: "crypto-secret-with-at-least-32-bytes", wantError: true},
		{name: "production missing crypto secret", production: true, cookieSecure: true, sessionSecret: "session-secret-with-at-least-32-bytes", wantError: true},
		{name: "production reused secret", production: true, cookieSecure: true, sessionSecret: "same-secret-with-at-least-32-bytes", cryptoSecret: "same-secret-with-at-least-32-bytes", wantError: true},
		{name: "production reused secret after trimming", production: true, cookieSecure: true, sessionSecret: " same-secret-with-at-least-32-bytes", cryptoSecret: "same-secret-with-at-least-32-bytes ", wantError: true},
		{name: "production rejects default session placeholder", production: true, cookieSecure: true, sessionSecret: "random_string", cryptoSecret: "crypto-secret-with-at-least-32-bytes", wantError: true},
		{name: "production rejects change me crypto placeholder", production: true, cookieSecure: true, sessionSecret: "session-secret-with-at-least-32-bytes", cryptoSecret: "change-me", wantError: true},
		{name: "production rejects short session secret", production: true, cookieSecure: true, sessionSecret: "short", cryptoSecret: "crypto-secret-with-at-least-32-bytes", wantError: true},
		{name: "production rejects short crypto secret", production: true, cookieSecure: true, sessionSecret: "session-secret-with-at-least-32-bytes", cryptoSecret: "short", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionSecurity(tt.production, tt.cookieSecure, tt.sessionSecret, tt.cryptoSecret)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestProductionDeploymentRequiresExplicitLocalOptIn(t *testing.T) {
	tests := []struct {
		value      string
		production bool
	}{
		{value: "", production: true},
		{value: "production", production: true},
		{value: "prod", production: true},
		{value: "staging", production: true},
		{value: "developmnt", production: true},
		{value: "development", production: false},
		{value: "DEV", production: false},
		{value: "local", production: false},
		{value: "test", production: false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			assert.Equal(t, tt.production, productionDeploymentForEnvironment(tt.value))
		})
	}
}
