package deployments

import (
	"testing"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestAuthEmailHookUsesGeneratedSigningSecret(t *testing.T) {
	project := newTestProject(testNamespace)
	project.Spec.Auth.EmailHook = &supabasev1alpha1.EmailHookSpec{
		Enabled: true,
		URI:     "https://email.sliqs.app/api/v1/supabase/auth",
	}
	secretNames := newTestSecretNames()
	secretNames.EmailHook = "my-app-email-hook"

	deployment := BuildAuthDeployment(project, secretNames)
	env := deployment.Spec.Template.Spec.Containers[0].Env

	wantValues := map[string]string{
		"GOTRUE_HOOK_SEND_EMAIL_ENABLED": "true",
		"GOTRUE_HOOK_SEND_EMAIL_URI":     "https://email.sliqs.app/api/v1/supabase/auth",
	}
	for name, want := range wantValues {
		found := false
		for _, variable := range env {
			if variable.Name == name {
				found = true
				if variable.Value != want {
					t.Fatalf("%s = %q, want %q", name, variable.Value, want)
				}
			}
		}
		if !found {
			t.Fatalf("missing %s", name)
		}
	}

	foundSecret := false
	for _, variable := range env {
		if variable.Name != "GOTRUE_HOOK_SEND_EMAIL_SECRETS" {
			continue
		}
		foundSecret = true
		if variable.ValueFrom == nil || variable.ValueFrom.SecretKeyRef == nil {
			t.Fatal("email hook secret is not sourced from a Secret")
		}
		ref := variable.ValueFrom.SecretKeyRef
		if ref.Name != "my-app-email-hook" || ref.Key != "secret" {
			t.Fatalf("email hook secret ref = %s/%s", ref.Name, ref.Key)
		}
	}
	if !foundSecret {
		t.Fatal("missing GOTRUE_HOOK_SEND_EMAIL_SECRETS")
	}
}
