package secrets

import (
	"strings"
	"testing"

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
