package secrets

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestGenerateEmailHookSecretUsesFixedContract(t *testing.T) {
	project := newTestProject("sliqs-dev")

	secret, name, err := GenerateEmailHookSecret(project)
	if err != nil {
		t.Fatalf("GenerateEmailHookSecret() error = %v", err)
	}
	if name != "my-app-email-hook" {
		t.Fatalf("name = %q, want %q", name, "my-app-email-hook")
	}
	if secret.Name != name || secret.Namespace != "sliqs-dev" {
		t.Fatalf("secret metadata = %s/%s, want sliqs-dev/%s", secret.Namespace, secret.Name, name)
	}
	value := string(secret.Data["secret"])
	if !strings.HasPrefix(value, "v1,whsec_") {
		t.Fatalf("secret value has unexpected format")
	}
	if len(secret.Data) != 1 {
		t.Fatalf("secret keys = %d, want 1", len(secret.Data))
	}
}

func TestValidateEmailHookSecretRejectsMalformedValues(t *testing.T) {
	t.Parallel()

	validValue := "v1,whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	tests := map[string]string{
		"empty payload":  "v1,whsec_",
		"invalid base64": "v1,whsec_not-base64",
		"wrong prefix":   strings.TrimPrefix(validValue, "v1,"),
	}

	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			secret := &corev1.Secret{Data: map[string][]byte{EmailHookSecretKey: []byte(value)}}
			if err := ValidateEmailHookSecret(secret); err == nil {
				t.Fatalf("ValidateEmailHookSecret() accepted %q", value)
			}
		})
	}

	valid := &corev1.Secret{Data: map[string][]byte{EmailHookSecretKey: []byte(validValue)}}
	if err := ValidateEmailHookSecret(valid); err != nil {
		t.Fatalf("ValidateEmailHookSecret() rejected a valid secret: %v", err)
	}

	preserved := &corev1.Secret{Data: map[string][]byte{EmailHookSecretKey: []byte(
		"v1,whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 58)),
	)}}
	if err := ValidateEmailHookSecret(preserved); err != nil {
		t.Fatalf("ValidateEmailHookSecret() rejected a valid preserved secret: %v", err)
	}
}

func newTestProject(namespace string) *supabasev1alpha1.SupabaseProject {
	return &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: namespace},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			Powersync: &supabasev1alpha1.PowersyncSpec{},
		},
	}
}

func TestPowersyncSecretNames(t *testing.T) {
	storagePwd, replPwd := PowersyncSecretNames(newTestProject("default"))
	if storagePwd != "my-app-powersync-storage-password" {
		t.Errorf("storagePwd = %q", storagePwd)
	}
	if replPwd != "my-app-powersync-replication-password" {
		t.Errorf("replPwd = %q", replPwd)
	}
}

func TestGeneratePowersyncSecrets(t *testing.T) {
	generated, err := GeneratePowersyncSecrets(newTestProject("test-ns"))
	if err != nil {
		t.Fatalf("GeneratePowersyncSecrets() error = %v", err)
	}
	if len(generated) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(generated))
	}
	if generated[0].StringData["username"] != "powersync_storage" {
		t.Errorf("storage username = %q", generated[0].StringData["username"])
	}
	if generated[1].StringData["username"] != "powersync_replication" {
		t.Errorf("replication username = %q", generated[1].StringData["username"])
	}
	if generated[0].StringData["password"] == generated[1].StringData["password"] {
		t.Error("storage and replication passwords must differ")
	}
}
