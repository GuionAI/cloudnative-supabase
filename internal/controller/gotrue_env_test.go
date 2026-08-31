package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestReconcileAuthValidatesGoTrueEnvSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		valueFrom  supabasev1alpha1.GoTrueEnvValueFrom
		objects    []client.Object
		wantErr    string
		wantDeploy bool
	}{
		{
			name: "missing required Secret",
			valueFrom: supabasev1alpha1.GoTrueEnvValueFrom{SecretKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{
				Name: "auth-settings",
				Key:  "phone-enabled",
			}},
			wantErr: "getting secret \"auth-settings\"",
		},
		{
			name: "missing required ConfigMap key",
			valueFrom: supabasev1alpha1.GoTrueEnvValueFrom{ConfigMapKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{
				Name: "auth-settings",
				Key:  "phone-enabled",
			}},
			objects: []client.Object{
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "auth-settings", Namespace: "default"}},
			},
			wantErr: "config map \"auth-settings\" is missing key \"phone-enabled\"",
		},
		{
			name: "optional missing Secret",
			valueFrom: supabasev1alpha1.GoTrueEnvValueFrom{SecretKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{
				Name:     "auth-settings",
				Key:      "phone-enabled",
				Optional: ptr.To(true),
			}},
			wantDeploy: true,
		},
		{
			name: "existing ConfigMap key",
			valueFrom: supabasev1alpha1.GoTrueEnvValueFrom{ConfigMapKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{
				Name: "auth-settings",
				Key:  "phone-enabled",
			}},
			objects: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "auth-settings", Namespace: "default"},
					Data:       map[string]string{"phone-enabled": "true"},
				},
			},
			wantDeploy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			project := &supabasev1alpha1.SupabaseProject{
				TypeMeta:   metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
				ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default", UID: "project-uid"},
				Spec: supabasev1alpha1.SupabaseProjectSpec{Auth: supabasev1alpha1.AuthSpec{
					GoTrueEnv: []supabasev1alpha1.GoTrueEnvVar{{
						Name:      "GOTRUE_EXTERNAL_PHONE_ENABLED",
						ValueFrom: tt.valueFrom,
					}},
				}},
			}
			objects := append([]client.Object{project}, tt.objects...)
			scheme := newIdempotencyTestScheme(t)
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project).WithObjects(objects...).Build()
			reconciler := &SupabaseProjectReconciler{Client: kubeClient, Scheme: scheme}

			err := reconciler.reconcileAuth(context.Background(), project, &supabasev1alpha1.SecretNamesStatus{})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("reconcileAuth() error = %v, want containing %q", err, tt.wantErr)
				}
				condition := meta.FindStatusCondition(project.Status.Conditions, supabasev1alpha1.ConditionTypeAuthReady)
				if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "GoTrueEnvSourceInvalid" {
					t.Fatalf("AuthReady condition = %#v, want GoTrueEnvSourceInvalid False", condition)
				}
				if project.Status.Services.Auth.Ready {
					t.Fatal("Auth service status is ready after an invalid GoTrue environment source")
				}
				return
			}
			if err != nil {
				t.Fatalf("reconcileAuth() error = %v", err)
			}
			deployment := &appsv1.Deployment{}
			err = kubeClient.Get(context.Background(), client.ObjectKey{Name: "app-auth", Namespace: "default"}, deployment)
			if tt.wantDeploy && err != nil {
				t.Fatalf("get Auth Deployment: %v", err)
			}
		})
	}
}
