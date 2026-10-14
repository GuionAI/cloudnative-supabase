package cnpg

import (
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestBuildPowerSyncPublication(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "test-ns"},
	}

	publication, err := BuildPowerSyncPublication(project)
	if err != nil {
		t.Fatal(err)
	}
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

func TestPublicationSchemas(t *testing.T) {
	for _, schemas := range [][]string{nil, {"public", "app_data"}, {"app_data"}, {"select", "_data", strings.Repeat("a", 63)}} {
		project := &supabasev1alpha1.SupabaseProject{Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{PublicationSchemas: schemas}}}
		publication, err := BuildPowerSyncPublication(project)
		if err != nil {
			t.Fatal(err)
		}
		want := schemas
		if want == nil {
			want = []string{"public"}
		}
		var got []string
		for _, object := range publication.Spec.Target.Objects {
			got = append(got, object.TablesInSchema)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("target = %v, want %v", got, want)
		}
		copied := project.DeepCopy()
		if schemas != nil {
			copied.Spec.Powersync.PublicationSchemas[0] = "changed"
			if project.Spec.Powersync.PublicationSchemas[0] == "changed" {
				t.Fatal("deepcopy shares schemas")
			}
		}
	}
	for _, schemas := range [][]string{{}, {""}, {"public", "public"}, {"Upper"}, {"a.b"}, {"a-b"}, {"1a"}, {"é"}, {strings.Repeat("a", 64)}, {`x"; DROP SCHEMA public;--`}, {"$(touch /tmp/unsafe)"}, {"x\nEOSQL"}} {
		project := &supabasev1alpha1.SupabaseProject{Spec: supabasev1alpha1.SupabaseProjectSpec{Powersync: &supabasev1alpha1.PowersyncSpec{PublicationSchemas: schemas}}}
		resource, err := BuildPowerSyncPublication(project)
		if err == nil || resource != nil {
			t.Fatalf("accepted invalid schemas %q", schemas)
		}
	}
}
