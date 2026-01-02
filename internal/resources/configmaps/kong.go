/*
Copyright 2026 GuionAI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package configmaps

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
	"github.com/GuionAI/cloudnative-supabase/internal/resources/common"
)

// KongConfigMapName returns the kong config map name
func KongConfigMapName(project *supabasev1alpha1.SupabaseProject) string {
	return project.Name + "-kong-config"
}

// BuildKongConfigMap creates the Kong declarative configuration ConfigMap
func BuildKongConfigMap(project *supabasev1alpha1.SupabaseProject) *corev1.ConfigMap {
	if project.Spec.Kong == nil {
		return nil
	}

	// Build service names
	authService := project.Name + "-auth"
	restService := project.Name + "-rest"
	metaService := project.Name + "-meta"

	config := fmt.Sprintf(`_format_version: "2.1"
_transform: true

###
### Consumers / Credentials
###
consumers:
  - username: DASHBOARD
  - username: anon
    keyauth_credentials:
      - key: ${SUPABASE_ANON_KEY}
  - username: service_role
    keyauth_credentials:
      - key: ${SUPABASE_SERVICE_KEY}

###
### Access Control Lists
###
acls:
  - consumer: DASHBOARD
    group: admin
  - consumer: anon
    group: anon
  - consumer: service_role
    group: admin

###
### API Routes
###
services:
  ## Auth Service
  - name: auth-v1-open
    url: http://%s:9999/verify
    routes:
      - name: auth-v1-open
        strip_path: true
        paths:
          - /auth/v1/verify
    plugins:
      - name: cors

  - name: auth-v1-open-callback
    url: http://%s:9999/callback
    routes:
      - name: auth-v1-open-callback
        strip_path: true
        paths:
          - /auth/v1/callback
    plugins:
      - name: cors

  - name: auth-v1-open-authorize
    url: http://%s:9999/authorize
    routes:
      - name: auth-v1-open-authorize
        strip_path: true
        paths:
          - /auth/v1/authorize
    plugins:
      - name: cors

  - name: auth-v1
    url: http://%s:9999
    routes:
      - name: auth-v1
        strip_path: true
        paths:
          - /auth/v1/
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: true
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon

  ## REST Service (PostgREST)
  - name: rest-v1
    url: http://%s:3000
    routes:
      - name: rest-v1
        strip_path: true
        paths:
          - /rest/v1/
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: true
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon

  ## GraphQL (via PostgREST)
  - name: graphql-v1
    url: http://%s:3000/rpc/graphql
    routes:
      - name: graphql-v1
        strip_path: true
        paths:
          - /graphql/v1
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: true
      - name: request-transformer
        config:
          add:
            headers:
              - Content-Profile:graphql_public
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon

  ## Meta Service (postgres-meta)
  - name: meta
    url: http://%s:8080
    routes:
      - name: meta
        strip_path: false
        paths:
          - /pg/
    plugins:
      - name: key-auth
        config:
          hide_credentials: true
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
`, authService, authService, authService, authService, restService, restService, metaService)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KongConfigMapName(project),
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "kong"),
		},
		Data: map[string]string{
			"kong.yml": config,
		},
	}
}
