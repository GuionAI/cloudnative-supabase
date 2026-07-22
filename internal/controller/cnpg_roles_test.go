package controller

import (
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestSyncManagedRolesAddsPowerSyncRoles(t *testing.T) {
	existing := &cnpgv1.Cluster{
		Spec: cnpgv1.ClusterSpec{
			Managed: &cnpgv1.ManagedConfiguration{
				Roles: []cnpgv1.RoleConfiguration{{Name: "supabase_admin"}},
			},
		},
	}
	desired := existing.DeepCopy()
	desired.Spec.Managed.Roles = append(desired.Spec.Managed.Roles,
		cnpgv1.RoleConfiguration{Name: "powersync_storage"},
		cnpgv1.RoleConfiguration{Name: "powersync_replication"},
	)

	if !syncManagedRoles(existing, desired) {
		t.Fatal("expected managed roles to change")
	}
	if got := len(existing.Spec.Managed.Roles); got != 3 {
		t.Fatalf("managed roles = %d, want 3", got)
	}
	if syncManagedRoles(existing, desired) {
		t.Fatal("expected identical managed roles to be a no-op")
	}
}

func TestPublicationIsApplied(t *testing.T) {
	if publicationIsApplied(&cnpgv1.Publication{}) {
		t.Fatal("publication without status must not be ready")
	}
	publication := &cnpgv1.Publication{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status:     cnpgv1.PublicationStatus{Applied: ptr.To(true), ObservedGeneration: 2},
	}
	if !publicationIsApplied(publication) {
		t.Fatal("publication with applied status must be ready")
	}
	publication.Status.ObservedGeneration = 1
	if publicationIsApplied(publication) {
		t.Fatal("publication with stale observed generation must not be ready")
	}
}
