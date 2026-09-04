package controller

import (
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestMergeManagedRolesAddsPowerSyncRoles(t *testing.T) {
	existing := []cnpgv1.RoleConfiguration{{Name: "supabase_admin"}}
	desired := append([]cnpgv1.RoleConfiguration(nil), existing...)
	desired = append(desired,
		cnpgv1.RoleConfiguration{Name: "powersync_storage"},
		cnpgv1.RoleConfiguration{Name: "powersync_replication"},
	)

	merged := mergeManagedRoles(existing, desired)
	if got := len(merged); got != 3 {
		t.Fatalf("managed roles = %d, want 3", got)
	}
	if got := mergeManagedRoles(merged, desired); len(got) != len(merged) {
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
