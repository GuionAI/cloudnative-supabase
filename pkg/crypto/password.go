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
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// GenerateHex generates n random bytes as a hex string (2n characters)
func GenerateHex(n int) (string, error) {
	bytes := make([]byte, n)
	bytesRead, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	if bytesRead != n {
		return "", fmt.Errorf("insufficient random bytes: got %d, want %d", bytesRead, n)
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateBase64 generates n random bytes as a base64 string
func GenerateBase64(n int) (string, error) {
	bytes := make([]byte, n)
	bytesRead, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	if bytesRead != n {
		return "", fmt.Errorf("insufficient random bytes: got %d, want %d", bytesRead, n)
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// GeneratePassword generates a secure password (32 bytes hex = 64 chars)
func GeneratePassword() (string, error) {
	return GenerateHex(32)
}

// GenerateWebhookSecret generates a webhook secret in Supabase format (v1,whsec_...)
func GenerateWebhookSecret() (string, error) {
	secret, err := GenerateBase64(32)
	if err != nil {
		return "", err
	}
	return "v1,whsec_" + secret, nil
}
