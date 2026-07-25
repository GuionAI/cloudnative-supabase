package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestReconcileSecretsCreatesAndPreservesEmailHookSecret(t *testing.T) {
	t.Parallel()

	scheme := newIdempotencyTestScheme(t)
	project := &supabasev1alpha1.SupabaseProject{
		TypeMeta: metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "app", Namespace: "default", UID: "project-uid",
		},
		Spec: supabasev1alpha1.SupabaseProjectSpec{Auth: supabasev1alpha1.AuthSpec{
			EmailHook: &supabasev1alpha1.EmailHookSpec{Enabled: true, URI: "https://email.example.com/auth"},
		}},
	}
	objects := []client.Object{project}
	for _, name := range []string{"app-jwt", "app-supabase-admin-password", "app-authenticator-password", "app-auth-admin-password"} {
		objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}})
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(objects...).Build()
	reconciler := &SupabaseProjectReconciler{Client: kubeClient, Scheme: scheme}

	if err := reconciler.reconcileSecrets(context.Background(), project); err != nil {
		t.Fatal(err)
	}

	created := &corev1.Secret{}
	key := client.ObjectKey{Name: "app-email-hook", Namespace: "default"}
	if err := kubeClient.Get(context.Background(), key, created); err != nil {
		t.Fatalf("get generated email hook secret: %v", err)
	}
	value := string(created.Data["secret"])
	if !strings.HasPrefix(value, "v1,whsec_") {
		t.Fatal("generated secret does not use the Standard Webhooks format")
	}
	if project.Status.SecretNames.EmailHook != "app-email-hook" {
		t.Fatalf("status emailHook = %q", project.Status.SecretNames.EmailHook)
	}

	if err := reconciler.reconcileSecrets(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	preserved := &corev1.Secret{}
	if err := kubeClient.Get(context.Background(), key, preserved); err != nil {
		t.Fatal(err)
	}
	preservedValue := string(preserved.Data["secret"])
	if preservedValue != value {
		t.Fatal("email hook secret rotated during reconciliation")
	}
}
