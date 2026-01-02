/*
Copyright 2026 GuionAI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package crypto

import (
	"strings"
	"testing"
)

func TestGenerateHex(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		wantLen int
	}{
		{"16 bytes", 16, 32},
		{"32 bytes", 32, 64},
		{"64 bytes", 64, 128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateHex(tt.n)
			if err != nil {
				t.Fatalf("GenerateHex() error = %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("GenerateHex() length = %d, want %d", len(got), tt.wantLen)
			}
			// Verify hex characters only
			for _, c := range got {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("GenerateHex() contains non-hex character: %c", c)
				}
			}
		})
	}
}

func TestGenerateHex_Uniqueness(t *testing.T) {
	// Generate multiple values and ensure they're unique
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		hex, err := GenerateHex(32)
		if err != nil {
			t.Fatalf("GenerateHex() error = %v", err)
		}
		if seen[hex] {
			t.Errorf("GenerateHex() produced duplicate value: %s", hex)
		}
		seen[hex] = true
	}
}

func TestGenerateBase64(t *testing.T) {
	got, err := GenerateBase64(32)
	if err != nil {
		t.Fatalf("GenerateBase64() error = %v", err)
	}
	// 32 bytes = 44 base64 characters (with padding)
	if len(got) != 44 {
		t.Errorf("GenerateBase64() length = %d, want 44", len(got))
	}
}

func TestGeneratePassword(t *testing.T) {
	password, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword() error = %v", err)
	}
	// 32 bytes = 64 hex characters
	if len(password) != 64 {
		t.Errorf("GeneratePassword() length = %d, want 64", len(password))
	}
}

func TestGenerateWebhookSecret(t *testing.T) {
	secret, err := GenerateWebhookSecret()
	if err != nil {
		t.Fatalf("GenerateWebhookSecret() error = %v", err)
	}

	// Should start with "v1,whsec_"
	if !strings.HasPrefix(secret, "v1,whsec_") {
		t.Errorf("GenerateWebhookSecret() = %s, should start with 'v1,whsec_'", secret)
	}

	// The base64 part should be 44 characters
	parts := strings.SplitN(secret, "v1,whsec_", 2)
	if len(parts) != 2 {
		t.Fatalf("GenerateWebhookSecret() format is invalid")
	}
	if len(parts[1]) != 44 {
		t.Errorf("GenerateWebhookSecret() base64 part length = %d, want 44", len(parts[1]))
	}
}
