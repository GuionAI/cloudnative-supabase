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

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"

	projectsecrets "github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
	projectcrypto "github.com/GuionAI/cloudnative-supabase/pkg/crypto"
)

const opaqueChecksumContext = "supabase-self-hosted"

var rawURL = base64.RawURLEncoding

type credentialBundle struct {
	SigningKeys    string `json:"signingKeys"`
	PublishableKey string `json:"publishableKey"`
	SecretKey      string `json:"secretKey"`
	AnonRoleJWT    string `json:"anonRoleJwt"`
	ServiceRoleJWT string `json:"serviceRoleJwt"`
}

func main() {
	bundle, err := generateCredentialBundle(time.Now(), rand.Reader)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "generate project credentials: %v\n", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(bundle); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "encode project credentials: %v\n", err)
		os.Exit(1)
	}
}

func generateCredentialBundle(now time.Time, random io.Reader) (credentialBundle, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), random)
	if err != nil {
		return credentialBundle{}, fmt.Errorf("generate ES256 key: %w", err)
	}
	kid, err := generateKeyID(random)
	if err != nil {
		return credentialBundle{}, err
	}

	ext := true
	jwk := projectcrypto.JWK{
		Kty:    "EC",
		Alg:    projectcrypto.ES256,
		Use:    "sig",
		Crv:    "P-256",
		X:      encodeCoordinate(privateKey.X),
		Y:      encodeCoordinate(privateKey.Y),
		D:      encodeCoordinate(privateKey.D),
		Kid:    kid,
		KeyOps: []string{"sign"},
		Ext:    &ext,
	}
	signingKeys, err := json.Marshal([]projectcrypto.JWK{jwk})
	if err != nil {
		return credentialBundle{}, fmt.Errorf("encode signing key: %w", err)
	}

	publishableKey, err := generateOpaqueKey("sb_publishable_", random)
	if err != nil {
		return credentialBundle{}, fmt.Errorf("generate publishable key: %w", err)
	}
	secretKey, err := generateOpaqueKey("sb_secret_", random)
	if err != nil {
		return credentialBundle{}, fmt.Errorf("generate secret key: %w", err)
	}

	expiresAt := now.AddDate(5, 0, 0).Unix()
	anonRoleJWT, err := signRoleJWT(privateKey, kid, "anon", now.Unix(), expiresAt, random)
	if err != nil {
		return credentialBundle{}, fmt.Errorf("sign anon role JWT: %w", err)
	}
	serviceRoleJWT, err := signRoleJWT(privateKey, kid, "service_role", now.Unix(), expiresAt, random)
	if err != nil {
		return credentialBundle{}, fmt.Errorf("sign service role JWT: %w", err)
	}

	bundle := credentialBundle{
		SigningKeys:    string(signingKeys),
		PublishableKey: publishableKey,
		SecretKey:      secretKey,
		AnonRoleJWT:    anonRoleJWT,
		ServiceRoleJWT: serviceRoleJWT,
	}
	if _, err := projectsecrets.ValidateProjectCredentials(bundle.secret()); err != nil {
		return credentialBundle{}, fmt.Errorf("generated bundle failed operator validation: %w", err)
	}
	return bundle, nil
}

func (bundle credentialBundle) secret() *corev1.Secret {
	return &corev1.Secret{StringData: map[string]string{
		projectsecrets.ProjectCredentialsSigningKeysKey:    bundle.SigningKeys,
		projectsecrets.ProjectCredentialsPublishableKey:    bundle.PublishableKey,
		projectsecrets.ProjectCredentialsSecretKey:         bundle.SecretKey,
		projectsecrets.ProjectCredentialsAnonRoleJWTKey:    bundle.AnonRoleJWT,
		projectsecrets.ProjectCredentialsServiceRoleJWTKey: bundle.ServiceRoleJWT,
	}}
}

func generateOpaqueKey(prefix string, random io.Reader) (string, error) {
	randomBytes := make([]byte, 17)
	if _, err := io.ReadFull(random, randomBytes); err != nil {
		return "", err
	}
	randomSegment := rawURL.EncodeToString(randomBytes)[:22]
	base := prefix + randomSegment
	digest := sha256.Sum256([]byte(opaqueChecksumContext + "|" + base))
	checksum := rawURL.EncodeToString(digest[:])[:8]
	return base + "_" + checksum, nil
}

func generateKeyID(random io.Reader) (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", fmt.Errorf("generate key ID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func signRoleJWT(
	privateKey *ecdsa.PrivateKey,
	kid, role string,
	issuedAt, expiresAt int64,
	random io.Reader,
) (string, error) {
	header, err := json.Marshal(projectcrypto.JWTHeader{
		Alg: projectcrypto.ES256,
		Typ: "JWT",
		Kid: kid,
	})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(projectcrypto.JWTClaims{
		Role: role,
		Aud:  projectcrypto.RequiredRoleAudience,
		Iss:  "supabase",
		Iat:  issuedAt,
		Exp:  expiresAt,
	})
	if err != nil {
		return "", err
	}

	message := rawURL.EncodeToString(header) + "." + rawURL.EncodeToString(claims)
	digest := sha256.Sum256([]byte(message))
	r, s, err := ecdsa.Sign(random, privateKey, digest[:])
	if err != nil {
		return "", err
	}
	signature := append(paddedInteger(r), paddedInteger(s)...)
	return message + "." + rawURL.EncodeToString(signature), nil
}

func encodeCoordinate(value *big.Int) string {
	return rawURL.EncodeToString(paddedInteger(value))
}

func paddedInteger(value *big.Int) []byte {
	result := make([]byte, 32)
	encoded := value.Bytes()
	copy(result[len(result)-len(encoded):], encoded)
	return result
}
