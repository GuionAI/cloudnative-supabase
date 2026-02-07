package secrets

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

const (
	testProjectName = "my-app"
	testNamespace   = "test-ns"
)

func newTestProject(namespace string) *supabasev1alpha1.SupabaseProject {
	return &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testProjectName,
			Namespace: namespace,
		},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			Sequin:      &supabasev1alpha1.SequinSpec{},
			Powersync:   &supabasev1alpha1.PowersyncSpec{},
			Meilisearch: &supabasev1alpha1.MeilisearchSpec{},
		},
	}
}

func TestSequinSecretNames(t *testing.T) {
	project := newTestProject("default")

	sequin, sequinPw, sequinRepl := SequinSecretNames(project)

	if sequin != "my-app-sequin" {
		t.Errorf("sequin = %q, want %q", sequin, "my-app-sequin")
	}
	if sequinPw != "my-app-sequin-password" {
		t.Errorf("sequinPassword = %q, want %q", sequinPw, "my-app-sequin-password")
	}
	if sequinRepl != "my-app-sequin-replication-password" {
		t.Errorf("sequinReplication = %q, want %q", sequinRepl, "my-app-sequin-replication-password")
	}
}

func TestPowersyncSecretNames(t *testing.T) {
	project := newTestProject("default")
	storagePwd, replPwd := PowersyncSecretNames(project)
	if storagePwd != "my-app-powersync-storage-password" {
		t.Errorf("storagePwd = %q, want %q", storagePwd, "my-app-powersync-storage-password")
	}
	if replPwd != "my-app-powersync-replication-password" {
		t.Errorf("replPwd = %q, want %q", replPwd, "my-app-powersync-replication-password")
	}
}

func TestMeilisearchSecretName(t *testing.T) {
	tests := []struct {
		name           string
		masterKeyRef   string
		wantSecretName string
	}{
		{
			name:           "auto-generated",
			masterKeyRef:   "",
			wantSecretName: "my-app-meilisearch-master-key",
		},
		{
			name:           "user-provided",
			masterKeyRef:   "my-existing-key",
			wantSecretName: "my-existing-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := newTestProject("default")
			project.Spec.Meilisearch.MasterKeySecretRef = tt.masterKeyRef

			got := MeilisearchSecretName(project)
			if got != tt.wantSecretName {
				t.Errorf("MeilisearchSecretName() = %q, want %q", got, tt.wantSecretName)
			}
		})
	}
}

func TestGenerateSequinSecrets(t *testing.T) {
	project := newTestProject(testNamespace)

	secrets, err := GenerateSequinSecrets(project)
	if err != nil {
		t.Fatalf("GenerateSequinSecrets() error = %v", err)
	}

	if len(secrets) != 3 {
		t.Fatalf("expected 3 secrets, got %d", len(secrets))
	}

	// Sequin app secret
	appSecret := secrets[0]
	if appSecret.Name != "my-app-sequin" {
		t.Errorf("app secret name = %q, want %q", appSecret.Name, "my-app-sequin")
	}
	if appSecret.Namespace != testNamespace {
		t.Errorf("app secret namespace = %q, want %q", appSecret.Namespace, testNamespace)
	}

	requiredAppKeys := []string{"secretKeyBase", "vaultKey", "apiToken"}
	for _, key := range requiredAppKeys {
		if _, ok := appSecret.StringData[key]; !ok {
			t.Errorf("app secret missing key: %s", key)
		}
	}

	// secretKeyBase should be 128 chars (64 bytes hex)
	if len(appSecret.StringData["secretKeyBase"]) != 128 {
		t.Errorf("secretKeyBase length = %d, want 128", len(appSecret.StringData["secretKeyBase"]))
	}

	// Sequin password secret
	pwSecret := secrets[1]
	if pwSecret.Name != "my-app-sequin-password" {
		t.Errorf("password secret name = %q, want %q", pwSecret.Name, "my-app-sequin-password")
	}
	if pwSecret.StringData["username"] != "sequin" {
		t.Errorf("username = %q, want %q", pwSecret.StringData["username"], "sequin")
	}

	// Replication password secret
	replSecret := secrets[2]
	if replSecret.Name != "my-app-sequin-replication-password" {
		t.Errorf("replication secret name = %q, want %q", replSecret.Name, "my-app-sequin-replication-password")
	}
	if replSecret.StringData["username"] != "sequin_replication" {
		t.Errorf("username = %q, want %q", replSecret.StringData["username"], "sequin_replication")
	}
}

func TestGeneratePowersyncSecrets(t *testing.T) {
	project := newTestProject(testNamespace)

	secrets, err := GeneratePowersyncSecrets(project)
	if err != nil {
		t.Fatalf("GeneratePowersyncSecrets() error = %v", err)
	}

	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}

	// Storage role secret
	storage := secrets[0]
	if storage.Name != "my-app-powersync-storage-password" {
		t.Errorf("storage name = %q, want %q", storage.Name, "my-app-powersync-storage-password")
	}
	if storage.StringData["username"] != "powersync_storage" {
		t.Errorf("storage username = %q, want %q", storage.StringData["username"], "powersync_storage")
	}

	// Replication role secret
	repl := secrets[1]
	if repl.Name != "my-app-powersync-replication-password" {
		t.Errorf("replication name = %q, want %q", repl.Name, "my-app-powersync-replication-password")
	}
	if repl.StringData["username"] != "powersync_replication" {
		t.Errorf("replication username = %q, want %q", repl.StringData["username"], "powersync_replication")
	}
}

func TestGenerateMeilisearchSecrets(t *testing.T) {
	project := newTestProject(testNamespace)

	secrets, err := GenerateMeilisearchSecrets(project)
	if err != nil {
		t.Fatalf("GenerateMeilisearchSecrets() error = %v", err)
	}

	if len(secrets) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(secrets))
	}

	secret := secrets[0]
	if secret.Name != "my-app-meilisearch-master-key" {
		t.Errorf("name = %q, want %q", secret.Name, "my-app-meilisearch-master-key")
	}
	if secret.Namespace != testNamespace {
		t.Errorf("namespace = %q, want %q", secret.Namespace, testNamespace)
	}

	// masterKey should be 64 chars (32 bytes hex)
	masterKey := secret.StringData["masterKey"]
	if len(masterKey) != 64 {
		t.Errorf("masterKey length = %d, want 64", len(masterKey))
	}
}

func TestGenerateMeilisearchSecrets_ExistingRef(t *testing.T) {
	project := newTestProject("default")
	project.Spec.Meilisearch.MasterKeySecretRef = "my-existing-key"

	secrets, err := GenerateMeilisearchSecrets(project)
	if err != nil {
		t.Fatalf("GenerateMeilisearchSecrets() error = %v", err)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets when MasterKeySecretRef is provided, got %d", len(secrets))
	}
}

func TestGenerateSecrets_Uniqueness(t *testing.T) {
	project := newTestProject("default")

	secrets1, err := GenerateSequinSecrets(project)
	if err != nil {
		t.Fatalf("first call error = %v", err)
	}

	secrets2, err := GenerateSequinSecrets(project)
	if err != nil {
		t.Fatalf("second call error = %v", err)
	}

	// Secrets should be different between calls (random generation)
	if secrets1[0].StringData["secretKeyBase"] == secrets2[0].StringData["secretKeyBase"] {
		t.Error("expected different secretKeyBase values between calls")
	}
}
