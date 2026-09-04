package controller

import (
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

var _ = Describe("GoTrue environment admission", func() {
	project := func(name string, env []supabasev1alpha1.GoTrueEnvVar) *supabasev1alpha1.SupabaseProject {
		return &supabasev1alpha1.SupabaseProject{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: supabasev1alpha1.SupabaseProjectSpec{
				ProjectCredentialsSecret: "project-credentials",
				Database:                 supabasev1alpha1.DatabaseSpec{Instances: 1, Storage: cnpgv1.StorageConfiguration{Size: "1Gi"}},
				Auth: supabasev1alpha1.AuthSpec{
					SiteURL:     "https://app.example.com",
					ExternalURL: "https://auth.example.com",
					GoTrueEnv:   env,
				},
			},
		}
	}

	It("accepts one literal or source and rejects ambiguous or empty values", func() {
		valid := project("gotrue-env-valid", []supabasev1alpha1.GoTrueEnvVar{
			{
				Name:  "GOTRUE_EXTERNAL_EMAIL_ENABLED",
				Value: ptr.To("true"),
			},
			{
				Name: "GOTRUE_EXTERNAL_PHONE_ENABLED",
				ValueFrom: &supabasev1alpha1.GoTrueEnvValueFrom{ConfigMapKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{
					Name: "auth-settings",
					Key:  "phone-enabled",
				}},
			},
			{
				Name: "GOTRUE_SMS_TWILIO_AUTH_TOKEN",
				ValueFrom: &supabasev1alpha1.GoTrueEnvValueFrom{SecretKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{
					Name: "twilio",
					Key:  "auth-token",
				}},
			},
		})
		Expect(k8sClient.Create(ctx, valid)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, valid)).To(Succeed()) })

		invalid := []*supabasev1alpha1.SupabaseProject{
			project("gotrue-env-no-source", []supabasev1alpha1.GoTrueEnvVar{{
				Name: "GOTRUE_EXTERNAL_PHONE_ENABLED",
			}}),
			project("gotrue-env-both-sources", []supabasev1alpha1.GoTrueEnvVar{{
				Name: "GOTRUE_EXTERNAL_PHONE_ENABLED",
				ValueFrom: &supabasev1alpha1.GoTrueEnvValueFrom{
					SecretKeyRef:    &supabasev1alpha1.GoTrueEnvKeySelector{Name: "auth-settings", Key: "phone-enabled"},
					ConfigMapKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{Name: "auth-settings", Key: "phone-enabled"},
				},
			}}),
			project("gotrue-env-literal-and-source", []supabasev1alpha1.GoTrueEnvVar{{
				Name:  "GOTRUE_EXTERNAL_PHONE_ENABLED",
				Value: ptr.To("true"),
				ValueFrom: &supabasev1alpha1.GoTrueEnvValueFrom{ConfigMapKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{
					Name: "auth-settings",
					Key:  "phone-enabled",
				}},
			}}),
			project("gotrue-env-empty-secret-reference", []supabasev1alpha1.GoTrueEnvVar{{
				Name:      "GOTRUE_EXTERNAL_PHONE_ENABLED",
				ValueFrom: &supabasev1alpha1.GoTrueEnvValueFrom{SecretKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{}},
			}}),
			project("gotrue-env-empty-configmap-reference", []supabasev1alpha1.GoTrueEnvVar{{
				Name:      "GOTRUE_EXTERNAL_PHONE_ENABLED",
				ValueFrom: &supabasev1alpha1.GoTrueEnvValueFrom{ConfigMapKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{}},
			}}),
			project("gotrue-env-duplicate-name", []supabasev1alpha1.GoTrueEnvVar{
				{
					Name:      "GOTRUE_EXTERNAL_PHONE_ENABLED",
					ValueFrom: &supabasev1alpha1.GoTrueEnvValueFrom{ConfigMapKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{Name: "auth-settings", Key: "phone-enabled"}},
				},
				{
					Name:      "GOTRUE_EXTERNAL_PHONE_ENABLED",
					ValueFrom: &supabasev1alpha1.GoTrueEnvValueFrom{ConfigMapKeyRef: &supabasev1alpha1.GoTrueEnvKeySelector{Name: "auth-settings", Key: "email-enabled"}},
				},
			}),
		}
		for _, candidate := range invalid {
			Expect(k8sClient.Create(ctx, candidate)).NotTo(Succeed(), candidate.Name)
		}
	})
})
