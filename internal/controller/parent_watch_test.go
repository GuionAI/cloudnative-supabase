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
