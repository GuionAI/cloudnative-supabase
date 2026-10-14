package controller

import (
	"fmt"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

var _ = Describe("PowerSync publication schema admission", func() {
	It("defaults omitted schemas and enforces the builder contract", func() {
		cases := []struct {
			schemas []string
			valid   bool
		}{
			{nil, true}, {[]string{"public", "app_data"}, true}, {[]string{"app_data"}, true}, {[]string{"select", "_data", strings.Repeat("a", 63)}, true},
			{[]string{}, false}, {[]string{""}, false}, {[]string{"public", "public"}, false}, {[]string{"Upper"}, false}, {[]string{"a.b"}, false}, {[]string{"a-b"}, false}, {[]string{"1a"}, false}, {[]string{"é"}, false}, {[]string{strings.Repeat("a", 64)}, false}, {[]string{`x"; DROP SCHEMA public;--`}, false}, {[]string{"$(touch /tmp/unsafe)"}, false},
		}
		for i, tc := range cases {
			candidate := &supabasev1alpha1.SupabaseProject{TypeMeta: metav1.TypeMeta{APIVersion: supabasev1alpha1.GroupVersion.String(), Kind: "SupabaseProject"}, ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("publication-schemas-%d", i), Namespace: "default"}, Spec: supabasev1alpha1.SupabaseProjectSpec{
				ProjectCredentialsSecret: "fixture-credentials", Database: supabasev1alpha1.DatabaseSpec{Instances: 1, Storage: cnpgv1.StorageConfiguration{Size: "1Gi"}}, Auth: supabasev1alpha1.AuthSpec{SiteURL: "https://app.example.com", ExternalURL: "https://auth.example.com"}, Powersync: &supabasev1alpha1.PowersyncSpec{PublicationSchemas: tc.schemas, SyncRules: supabasev1alpha1.SyncRulesSpec{Inline: "config:\n  edition: 3\n"}},
			}}
			_, builderErr := candidate.EffectivePublicationSchemas()
			Expect(builderErr == nil).To(Equal(tc.valid))
			raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(candidate)
			Expect(err).NotTo(HaveOccurred())
			// omitempty erases empty Go slices; preserve explicit [] at the API boundary.
			if tc.schemas != nil {
				Expect(unstructured.SetNestedStringSlice(raw, tc.schemas, "spec", "powersync", "publicationSchemas")).To(Succeed())
			}
			resource := &unstructured.Unstructured{Object: raw}
			err = k8sClient.Create(ctx, resource)
			if !tc.valid {
				Expect(err).To(HaveOccurred(), "schemas: %v", tc.schemas)
				continue
			}
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(k8sClient.Delete(ctx, resource)).To(Succeed()) })
			got, found, err := unstructured.NestedStringSlice(resource.Object, "spec", "powersync", "publicationSchemas")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			want := tc.schemas
			if want == nil {
				want = []string{"public"}
			}
			Expect(got).To(Equal(want))
		}
	})
})
