package deployments

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

const testNamespace = "test-ns"

func newTestProject(namespace string) *supabasev1alpha1.SupabaseProject {
	return &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: namespace},
		Spec: supabasev1alpha1.SupabaseProjectSpec{
			Powersync: &supabasev1alpha1.PowersyncSpec{
				Compact: supabasev1alpha1.PowersyncCompactSpec{Enabled: true},
			},
		},
	}
}

func newTestSecretNames() *supabasev1alpha1.SecretNamesStatus {
	return &supabasev1alpha1.SecretNamesStatus{
		JWT:                          "test-jwt",
		PowersyncStoragePassword:     "test-powersync-storage-password",
		PowersyncReplicationPassword: "test-powersync-replication-password",
	}
}
