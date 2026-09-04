package controller

import (
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

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
