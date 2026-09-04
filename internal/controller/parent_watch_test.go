package controller

import (
	"context"
	"sync/atomic"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	deploymentresources "github.com/GuionAI/cloudnative-supabase/internal/resources/deployments"
)

type projectGetCountingClient struct {
	client.Client
	projectGets atomic.Int32
}

func (c *projectGetCountingClient) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	if _, ok := object.(*supabasev1alpha1.SupabaseProject); ok {
		c.projectGets.Add(1)
	}
	return c.Client.Get(ctx, key, object, options...)
}

var _ = Describe("SupabaseProject parent watch", func() {
	It("reconciles removed deployment fields once despite API defaults", func() {
		project := &supabasev1alpha1.SupabaseProject{
			TypeMeta:   metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
			ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: "default", UID: "defaults-project-uid"},
			Spec: supabasev1alpha1.SupabaseProjectSpec{Auth: supabasev1alpha1.AuthSpec{
				EmailHook: &supabasev1alpha1.EmailHookSpec{Enabled: true, URI: "https://hook.example.com"},
			}, ProjectCredentialsSecret: "credentials"},
		}
		secretNames := &supabasev1alpha1.SecretNamesStatus{
			GoTrueFallback: "defaults-gotrue-jwt-secret", AuthAdmin: "defaults-auth-admin", Authenticator: "defaults-authenticator",
		}
		desired := deploymentresources.BuildAuthDeployment(project, secretNames)
		Expect(setTestControllerReference(project, desired, k8sClient.Scheme())).To(Succeed())
		Expect(k8sClient.Create(ctx, desired)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, desired))).To(Succeed()) })

		freshDesired := deploymentresources.BuildAuthDeployment(project, secretNames)
		countingClient := &updateCountingClient{Client: k8sClient}
		reconciler := &SupabaseProjectReconciler{Client: countingClient, Scheme: k8sClient.Scheme()}
		Expect(reconciler.createOrUpdateDeployment(ctx, project, freshDesired)).To(Succeed())
		Expect(countingClient.updates.Load()).To(BeZero())

		project.Spec.Auth.EmailHook.Enabled = false
		disabled := deploymentresources.BuildAuthDeployment(project, secretNames)
		Expect(reconciler.createOrUpdateDeployment(ctx, project, disabled)).To(Succeed())
		Expect(countingClient.updates.Load()).To(Equal(int32(1)))
		Expect(reconciler.createOrUpdateDeployment(ctx, project, disabled)).To(Succeed())
		Expect(countingClient.updates.Load()).To(Equal(int32(1)))
	})

	It("does not update a CronJob only because the API server defaulted it", func() {
		project := &supabasev1alpha1.SupabaseProject{
			TypeMeta:   metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"},
			ObjectMeta: metav1.ObjectMeta{Name: "cron-defaults", Namespace: "default", UID: "cron-defaults-project-uid"},
			Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{
				Compact: supabasev1alpha1.PowersyncCompactSpec{Enabled: true},
			}},
		}
		secretNames := &supabasev1alpha1.SecretNamesStatus{
			GoTrueFallback: "cron-defaults-gotrue-jwt-secret", PowersyncStoragePassword: "cron-defaults-storage",
			PowersyncReplicationPassword: "cron-defaults-replication",
		}
		desired := deploymentresources.BuildPowersyncCompactCronJob(project, secretNames)
		Expect(setTestControllerReference(project, desired, k8sClient.Scheme())).To(Succeed())
		Expect(k8sClient.Create(ctx, desired)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, desired))).To(Succeed()) })

		freshDesired := deploymentresources.BuildPowersyncCompactCronJob(project, secretNames)
		countingClient := &updateCountingClient{Client: k8sClient}
		reconciler := &SupabaseProjectReconciler{Client: countingClient, Scheme: k8sClient.Scheme()}
		Expect(reconciler.createOrUpdateCronJob(ctx, project, freshDesired)).To(Succeed())
		Expect(countingClient.updates.Load()).To(BeZero())
	})

	It("ignores status-only updates but reconciles spec changes", func() {
		manager, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 k8sClient.Scheme(),
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
		})
		Expect(err).NotTo(HaveOccurred())

		countingClient := &projectGetCountingClient{
			Client: fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).Build(),
		}
		reconciler := &SupabaseProjectReconciler{Client: countingClient, Scheme: k8sClient.Scheme()}
		Expect(reconciler.SetupWithManager(manager)).To(Succeed())

		managerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			defer GinkgoRecover()
			Expect(manager.Start(managerCtx)).To(Succeed())
		}()
		Expect(manager.GetCache().WaitForCacheSync(managerCtx)).To(BeTrue())

		project := &supabasev1alpha1.SupabaseProject{
			ObjectMeta: metav1.ObjectMeta{Name: "parent-watch", Namespace: "default"},
			Spec: supabasev1alpha1.SupabaseProjectSpec{
				Database: supabasev1alpha1.DatabaseSpec{
					Instances: 1,
					Storage:   cnpgv1.StorageConfiguration{Size: "1Gi"},
				},
				Auth: supabasev1alpha1.AuthSpec{
					SiteURL:     "https://app.example.com",
					ExternalURL: "https://auth.example.com",
				},
				ProjectCredentialsSecret: "parent-watch-credentials",
			},
		}
		Expect(k8sClient.Create(ctx, project)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, project))).To(Succeed()) })

		Eventually(countingClient.projectGets.Load, 10*time.Second).Should(BeNumerically(">=", 1))
		baseline := countingClient.projectGets.Load()

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(project), project)).To(Succeed())
		project.Status.Phase = supabasev1alpha1.PhaseRunning
		Expect(k8sClient.Status().Update(ctx, project)).To(Succeed())
		Consistently(countingClient.projectGets.Load, time.Second).Should(Equal(baseline))

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(project), project)).To(Succeed())
		project.Spec.Auth.SiteURL = "https://new.example.com"
		Expect(k8sClient.Update(ctx, project)).To(Succeed())
		Eventually(countingClient.projectGets.Load, 10*time.Second).Should(BeNumerically(">", baseline))
	})
})
