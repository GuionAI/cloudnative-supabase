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

const (
	// EnvoyConfigComponentName labels all gateway configuration assets.
	EnvoyConfigComponentName = "gateway-config"
	// EnvoyUpstreamCommit identifies the official Supabase Envoy asset source.
	EnvoyUpstreamCommit = "95ca3024398080ff18c9abcd1c6c8beae73fd9e1"
	// JWKSDataKey is the ConfigMap key consumed by PostgREST.
	JWKSDataKey = "jwks.json"
)

// BuildEnvoyConfigMap returns the pinned upstream Envoy asset set adapted to
// the managed service profile (Auth, REST, Studio, Meta, and Envoy).
func BuildEnvoyConfigMap(project *supabasev1alpha1.SupabaseProject) *corev1.ConfigMap {
	auth := project.Name + "-auth"
	rest := project.Name + "-rest"
	meta := project.Name + "-meta"
	studio := project.Name + "-studio"
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.EnvoyConfigMapName(project),
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, EnvoyConfigComponentName),
		},
		Data: map[string]string{
			"envoy.yaml":           envoyBootstrap,
			"cds.yaml":             fmt.Sprintf(envoyCDS, auth, rest, meta, studio),
			"lds.template.yaml":    envoyLDS,
			"docker-entrypoint.sh": envoyEntrypoint,
		},
	}
}

// BuildJWKSConfigMap returns the public-only Auth verifier projection.
func BuildJWKSConfigMap(project *supabasev1alpha1.SupabaseProject, jwks string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.JWKSConfigMapName(project),
			Namespace: project.Namespace,
			Labels:    common.ComponentLabels(project, "auth-jwks"),
		},
		Data: map[string]string{JWKSDataKey: jwks},
	}
}

const envoyBootstrap = `dynamic_resources:
  cds_config:
    path_config_source:
      path: /etc/envoy/cds.yaml
    resource_api_version: V3
  lds_config:
    path_config_source:
      path: /etc/envoy/lds.yaml
    resource_api_version: V3
node:
  cluster: supabase_cluster
  id: supabase_node

overload_manager:
  resource_monitors:
    - name: envoy.resource_monitors.global_downstream_max_connections
      typed_config:
        '@type': type.googleapis.com/envoy.extensions.resource_monitors.downstream_connections.v3.DownstreamConnectionsConfig
        max_active_downstream_connections: 30000

admin:
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 9901
`

const envoyCDS = `resources:
  - '@type': type.googleapis.com/envoy.config.cluster.v3.Cluster
    name: auth
    connect_timeout: 5s
    type: STRICT_DNS
    dns_refresh_rate: 5s
    dns_failure_refresh_rate:
      base_interval: 1s
      max_interval: 1s
    lb_policy: ROUND_ROBIN
    load_assignment:
      cluster_name: auth
      endpoints:
        - lb_endpoints:
            - endpoint:
                address:
                  socket_address:
                    address: %s
                    port_value: 9999
    health_checks:
      - timeout: 2s
        interval: 5s
        unhealthy_threshold: 3
        healthy_threshold: 2
        http_health_check:
          path: /health
    circuit_breakers:
      thresholds:
        - priority: DEFAULT
          max_connections: 10000
          max_pending_requests: 10000
          max_requests: 10000
  - '@type': type.googleapis.com/envoy.config.cluster.v3.Cluster
    name: rest
    connect_timeout: 5s
    type: STRICT_DNS
    dns_refresh_rate: 5s
    dns_failure_refresh_rate:
      base_interval: 1s
      max_interval: 1s
    lb_policy: ROUND_ROBIN
    load_assignment:
      cluster_name: rest
      endpoints:
        - lb_endpoints:
            - endpoint:
                address:
                  socket_address:
                    address: %s
                    port_value: 3000
    health_checks:
      - timeout: 2s
        interval: 5s
        unhealthy_threshold: 3
        healthy_threshold: 2
        http_health_check:
          path: /
    circuit_breakers:
      thresholds:
        - priority: DEFAULT
          max_connections: 10000
          max_pending_requests: 10000
          max_requests: 10000
  - '@type': type.googleapis.com/envoy.config.cluster.v3.Cluster
    name: meta
    connect_timeout: 5s
    type: STRICT_DNS
    dns_refresh_rate: 5s
    dns_failure_refresh_rate:
      base_interval: 1s
      max_interval: 1s
    lb_policy: ROUND_ROBIN
    load_assignment:
      cluster_name: meta
      endpoints:
        - lb_endpoints:
            - endpoint:
                address:
                  socket_address:
                    address: %s
                    port_value: 8080
    health_checks:
      - timeout: 2s
        interval: 5s
        unhealthy_threshold: 3
        healthy_threshold: 2
        http_health_check:
          path: /health
    circuit_breakers:
      thresholds:
        - priority: DEFAULT
          max_connections: 10000
          max_pending_requests: 10000
          max_requests: 10000
  - '@type': type.googleapis.com/envoy.config.cluster.v3.Cluster
    name: studio
    connect_timeout: 5s
    type: STRICT_DNS
    dns_refresh_rate: 5s
    dns_failure_refresh_rate:
      base_interval: 1s
      max_interval: 1s
    lb_policy: ROUND_ROBIN
    load_assignment:
      cluster_name: studio
      endpoints:
        - lb_endpoints:
            - endpoint:
                address:
                  socket_address:
                    address: %s
                    port_value: 3000
    health_checks:
      - timeout: 2s
        interval: 5s
        unhealthy_threshold: 3
        healthy_threshold: 2
        http_health_check:
          path: /project/default
    circuit_breakers:
      thresholds:
        - priority: DEFAULT
          max_connections: 10000
          max_pending_requests: 10000
          max_requests: 10000
`

// envoyLDS is rendered in the gateway container. The Lua filter deliberately
// contains only opaque key placeholders and their corresponding internal role
// JWT placeholders; there is no legacy JWT-shaped key path.
const envoyLDS = `resources:
  - '@type': type.googleapis.com/envoy.config.listener.v3.Listener
    name: supabase
    per_connection_buffer_limit_bytes: 32768
    address:
      socket_address:
        address: 0.0.0.0
        port_value: 8000
    filter_chains:
      - filters:
          - name: envoy.filters.network.http_connection_manager
            typed_config:
              '@type': type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
              stat_prefix: supabase_http
              normalize_path: true
              merge_slashes: true
              path_with_escaped_slashes_action: REJECT_REQUEST
              use_remote_address: true
              common_http_protocol_options:
                headers_with_underscores_action: REJECT_REQUEST
              upgrade_configs:
                - upgrade_type: websocket
              route_config:
                name: supabase_routes
                virtual_hosts:
                  - name: supabase
                    domains: ['*']
                    routes:
                      - match: { prefix: /auth/v1/.well-known/jwks.json }
                        route: { cluster: auth, prefix_rewrite: /.well-known/jwks.json }
                      - match: { prefix: /auth/v1/ }
                        route: { cluster: auth, prefix_rewrite: / }
                      - match: { prefix: /rest/v1/ }
                        route: { cluster: rest, prefix_rewrite: / }
                      - match: { prefix: /pg/ }
                        route: { cluster: meta, prefix_rewrite: / }
                      - match: { prefix: / }
                        route: { cluster: studio }
              http_filters:
                - name: envoy.filters.http.cors
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.cors.v3.Cors
                - name: envoy.filters.http.lua
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
                    inline_code: |
                      local function starts_with(value, prefix)
                        return value ~= nil and string.sub(value, 1, string.len(prefix)) == prefix
                      end

                      local function is_public_path(path)
                        if path == nil then
                          return false
                        end
                        return starts_with(path, "/auth/v1/.well-known/jwks.json")
                          or starts_with(path, "/.well-known/oauth-authorization-server")
                          or starts_with(path, "/auth/v1/verify")
                          or starts_with(path, "/auth/v1/callback")
                          or starts_with(path, "/auth/v1/authorize")
                          or starts_with(path, "/auth/v1/sso/saml/acs")
                          or starts_with(path, "/auth/v1/sso/saml/metadata")
                      end

                      local function query_apikey(path)
                        if path == nil then
                          return nil
                        end
                        local query_start = string.find(path, "?", 1, true)
                        if query_start == nil then
                          return nil
                        end
                        local query = string.sub(path, query_start + 1)
                        for key, value in string.gmatch(query, "([^&=]+)=([^&]*)") do
                          if key == "apikey" and value ~= "" then
                            return value
                          end
                        end
                        return nil
                      end

                      local function replace_query_apikey(path, replacement)
                        if path == nil or replacement == nil then
                          return path
                        end
                        local query_start = string.find(path, "?", 1, true)
                        if query_start == nil then
                          return path
                        end
                        local base = string.sub(path, 1, query_start)
                        local query = string.sub(path, query_start + 1)
                        local parts = {}
                        local replaced = false
                        for part in string.gmatch(query, "([^&]+)") do
                          local key = string.match(part, "^([^=]+)=")
                          if key == "apikey" then
                            part = key .. "=" .. replacement
                            replaced = true
                          end
                          table.insert(parts, part)
                        end
                        if not replaced then
                          return path
                        end
                        return base .. table.concat(parts, "&")
                      end

                      function envoy_on_request(request_handle)
                        local headers = request_handle:headers()
                        local path = headers:get(":path")
                        if is_public_path(path) then
                          return
                        end
                        local key = headers:get("apikey")
                        if key == nil then
                          key = query_apikey(path)
                          if key ~= nil then
                            headers:add("apikey", key)
                          end
                        end
                        if key == nil then
                          request_handle:respond({[":status"] = "401"}, "missing API key")
                          return
                        end
                        if key == "${SUPABASE_PUBLISHABLE_KEY}" then
                          headers:replace("apikey", "${SUPABASE_ANON_ROLE_JWT}")
                          key = "${SUPABASE_ANON_ROLE_JWT}"
                        elseif key == "${SUPABASE_SECRET_KEY}" then
                          headers:replace("apikey", "${SUPABASE_SERVICE_ROLE_JWT}")
                          key = "${SUPABASE_SERVICE_ROLE_JWT}"
                        else
                          request_handle:respond({[":status"] = "401"}, "invalid API key")
                          return
                        end
                        headers:replace(":path", replace_query_apikey(path, key))
                        local authorization = headers:get("authorization")
                        if authorization == nil or string.sub(authorization, 1, 7) ~= "Bearer " then
                          headers:replace("authorization", "Bearer " .. key)
                        end
                      end
                - name: envoy.filters.http.router
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
`

const envoyEntrypoint = `#!/bin/sh
set -eu

escape_sed() {
  printf '%s' "$1" | sed 's/[\\&|]/\\\\&/g'
}
publishable=$(escape_sed "${SUPABASE_PUBLISHABLE_KEY}")
secret=$(escape_sed "${SUPABASE_SECRET_KEY}")
anon=$(escape_sed "${SUPABASE_ANON_ROLE_JWT}")
service=$(escape_sed "${SUPABASE_SERVICE_ROLE_JWT}")
sed -e "s|\${SUPABASE_PUBLISHABLE_KEY}|$publishable|g" \
    -e "s|\${SUPABASE_SECRET_KEY}|$secret|g" \
    -e "s|\${SUPABASE_ANON_ROLE_JWT}|$anon|g" \
    -e "s|\${SUPABASE_SERVICE_ROLE_JWT}|$service|g" \
    /etc/envoy/lds.template.yaml > /etc/envoy/lds.yaml
exec envoy -c /etc/envoy/envoy.yaml
`
