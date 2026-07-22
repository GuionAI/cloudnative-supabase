package cnpg

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestBuildPowerSyncPublication(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "test-ns"},
	}

	publication := BuildPowerSyncPublication(project)
	if publication.Name != "my-app-powersync" || publication.Namespace != "test-ns" {
		t.Fatalf("unexpected identity: %s/%s", publication.Namespace, publication.Name)
	}
	if publication.Spec.ClusterRef.Name != "my-app-pg" {
		t.Errorf("cluster = %q", publication.Spec.ClusterRef.Name)
	}
	if publication.Spec.Name != "powersync" || publication.Spec.DBName != "supabase" {
		t.Errorf("unexpected PostgreSQL publication: %#v", publication.Spec)
	}
	if len(publication.Spec.Target.Objects) != 1 || publication.Spec.Target.Objects[0].TablesInSchema != "public" {
		t.Errorf("unexpected target: %#v", publication.Spec.Target)
	}
}
