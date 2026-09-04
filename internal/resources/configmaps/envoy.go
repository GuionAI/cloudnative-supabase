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
      # The admin API includes /config_dump with the fully rendered opaque
      # credentials and role JWTs. Keep it in the Envoy container only.
      address: 127.0.0.1
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

// envoyLDS is the pinned upstream Envoy listener/filter profile adapted to the
// services this operator actually manages. It intentionally omits routes for
// services outside that profile. The Lua filters retain upstream's ordering:
// query normalization, opaque-key admission, query/header translation, bearer
// synthesis, then RBAC.
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
              stat_prefix: ingress_http
              normalize_path: true
              merge_slashes: true
              path_with_escaped_slashes_action: REJECT_REQUEST
              use_remote_address: true
              common_http_protocol_options:
                headers_with_underscores_action: REJECT_REQUEST
              upgrade_configs:
                - upgrade_type: websocket
              access_log:
                - name: envoy.access_loggers.stdout
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StdoutAccessLog
                    log_format:
                      text_format_source:
                        inline_string: "%DOWNSTREAM_REMOTE_ADDRESS_WITHOUT_PORT% - - [%START_TIME(%d/%b/%Y:%H:%M:%S %z)%] \"%REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% %PROTOCOL%\" %RESPONSE_CODE% %BYTES_SENT% \"%REQ(REFERER)%\" \"%REQ(USER-AGENT)%\"\n"
              route_config:
                name: supabase_route
                virtual_hosts:
                  - name: supabase_host
                    domains: ['*']
                    cors:
                      allow_origin_string_match:
                        - safe_regex:
                            regex: ".*"
                      allow_methods: "GET,POST,PUT,PATCH,DELETE,OPTIONS,HEAD,CONNECT,TRACE"
                      allow_headers: "*"
                      expose_headers: "*"
                      max_age: "3600"
                    request_headers_to_add:
                      - header:
                          key: X-Forwarded-Host
                          value: "%REQ(:AUTHORITY)%"
                        append_action: ADD_IF_ABSENT
                      - header:
                          key: X-Forwarded-Port
                          value: "%DOWNSTREAM_LOCAL_PORT%"
                        append_action: ADD_IF_ABSENT
                    routes:
                      # This direct response is intentionally the only public
                      # probe. Envoy admin remains loopback-only because
                      # /config_dump contains rendered credential material.
                      - name: internal-health
                        match:
                          path: /_internal/health
                        direct_response:
                          status: 200
                          body:
                            inline_string: "ok\\n"
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  allow_all:
                                    permissions:
                                      - any: true
                                    principals:
                                      - any: true

                      # Auth discovery/callback endpoints are public by
                      # design; protected Auth traffic is below them.
                      - name: auth-v1-verify
                        match:
                          prefix: /auth/v1/verify
                        route:
                          cluster: auth
                          prefix_rewrite: /verify
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /auth/v1/verify
                            append_action: ADD_IF_ABSENT
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  allow_all:
                                    permissions:
                                      - any: true
                                    principals:
                                      - any: true
                      - name: auth-v1-callback
                        match:
                          prefix: /auth/v1/callback
                        route:
                          cluster: auth
                          prefix_rewrite: /callback
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /auth/v1/callback
                            append_action: ADD_IF_ABSENT
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  allow_all:
                                    permissions:
                                      - any: true
                                    principals:
                                      - any: true
                      - name: auth-v1-authorize
                        match:
                          prefix: /auth/v1/authorize
                        route:
                          cluster: auth
                          prefix_rewrite: /authorize
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /auth/v1/authorize
                            append_action: ADD_IF_ABSENT
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  allow_all:
                                    permissions:
                                      - any: true
                                    principals:
                                      - any: true
                      - name: auth-v1-jwks
                        match:
                          prefix: /auth/v1/.well-known/jwks.json
                        route:
                          cluster: auth
                          prefix_rewrite: /.well-known/jwks.json
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /auth/v1/.well-known/jwks.json
                            append_action: ADD_IF_ABSENT
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  allow_all:
                                    permissions:
                                      - any: true
                                    principals:
                                      - any: true
                      - name: oauth-authorization-server
                        match:
                          prefix: /.well-known/oauth-authorization-server
                        route:
                          cluster: auth
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /.well-known/oauth-authorization-server
                            append_action: ADD_IF_ABSENT
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  allow_all:
                                    permissions:
                                      - any: true
                                    principals:
                                      - any: true
                      - name: auth-v1-saml-acs
                        match:
                          prefix: /auth/v1/sso/saml/acs
                        route:
                          cluster: auth
                          prefix_rewrite: /sso/saml/acs
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /auth/v1/sso/saml/acs
                            append_action: ADD_IF_ABSENT
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  allow_all:
                                    permissions:
                                      - any: true
                                    principals:
                                      - any: true
                      - name: auth-v1-saml-metadata
                        match:
                          prefix: /auth/v1/sso/saml/metadata
                        route:
                          cluster: auth
                          prefix_rewrite: /sso/saml/metadata
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /auth/v1/sso/saml/metadata
                            append_action: ADD_IF_ABSENT
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  allow_all:
                                    permissions:
                                      - any: true
                                    principals:
                                      - any: true

                      - name: auth-v1-protected
                        match:
                          prefix: /auth/v1/
                        route:
                          cluster: auth
                          prefix_rewrite: /
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /auth/v1/
                            append_action: ADD_IF_ABSENT

                      - name: rest-v1-openapi-protected
                        match:
                          path: /rest/v1/
                        route:
                          cluster: rest
                          prefix_rewrite: /
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /rest/v1/
                            append_action: ADD_IF_ABSENT
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  service-role:
                                    permissions:
                                      - any: true
                                    principals:
                                      - header:
                                          name: apikey
                                          string_match:
                                            exact: '${SUPABASE_SERVICE_ROLE_JWT}'

                      - name: rest-v1-protected
                        match:
                          prefix: /rest/v1/
                        route:
                          cluster: rest
                          prefix_rewrite: /
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /rest/v1/
                            append_action: ADD_IF_ABSENT

                      - name: pg-protected
                        match:
                          prefix: /pg/
                        route:
                          cluster: meta
                          prefix_rewrite: /
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /pg/
                            append_action: ADD_IF_ABSENT

                      # Studio is the dashboard catch-all. Its own session
                      # handling is not part of the API-key boundary.
                      - name: studio
                        match:
                          prefix: /
                        route:
                          cluster: studio
                          timeout: 30s
                        request_headers_to_add:
                          - header:
                              key: X-Forwarded-Prefix
                              value: /
                            append_action: ADD_IF_ABSENT
                        request_headers_to_remove:
                          - authorization
                        typed_per_filter_config:
                          envoy.filters.http.rbac:
                            '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute
                            rbac:
                              rules:
                                action: ALLOW
                                policies:
                                  allow_all:
                                    permissions:
                                      - any: true
                                    principals:
                                      - any: true

              http_filters:
                # Envoy's native CORS filter handles OPTIONS preflight before
                # the authentication Lua filters run.
                - name: envoy.filters.http.cors
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.cors.v3.Cors

                # Copy ?apikey=... to the header for clients that cannot set
                # headers. Protected-route validation happens below.
                - name: envoy.filters.http.lua
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
                    inline_code: |
                      local function protected_route(request_handle)
                        local route = request_handle:streamInfo():routeName()
                        return route == "auth-v1-protected"
                          or route == "rest-v1-openapi-protected"
                          or route == "rest-v1-protected"
                          or route == "pg-protected"
                      end

                      function envoy_on_request(request_handle)
                        if not protected_route(request_handle) then
                          return
                        end
                        local headers = request_handle:headers()
                        if headers:get("apikey") ~= nil then
                          return
                        end
                        local path = headers:get(":path")
                        if path == nil then
                          return
                        end
                        local query_start = string.find(path, "?", 1, true)
                        if query_start == nil then
                          return
                        end
                        local query = string.sub(path, query_start + 1)
                        for key, value in string.gmatch(query, "([^&=]+)=([^&]*)") do
                          if key == "apikey" and value ~= "" then
                            headers:add("apikey", value)
                            return
                          end
                        end
                      end

                # Admit only the two configured opaque keys. This runs before
                # translation so internal role JWTs cannot be supplied as API
                # keys and no JWT-shaped legacy key path exists.
                - name: envoy.filters.http.lua
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
                    inline_code: |
                      local PUBLISHABLE_KEY = "${SUPABASE_PUBLISHABLE_KEY}"
                      local SECRET_KEY = "${SUPABASE_SECRET_KEY}"

                      local function protected_route(request_handle)
                        local route = request_handle:streamInfo():routeName()
                        return route == "auth-v1-protected"
                          or route == "rest-v1-openapi-protected"
                          or route == "rest-v1-protected"
                          or route == "pg-protected"
                      end

                      function envoy_on_request(request_handle)
                        if not protected_route(request_handle) then
                          return
                        end
                        local key = request_handle:headers():get("apikey")
                        if key ~= PUBLISHABLE_KEY and key ~= SECRET_KEY then
                          request_handle:respond({
                            [":status"] = "401",
                            ["content-type"] = "text/plain",
                          }, "Unauthorized")
                        end
                      end

                # Translate a query apikey and replace the opaque value in the
                # URL with the corresponding internal role token.
                - name: envoy.filters.http.lua
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
                    inline_code: |
                      local PUBLISHABLE_KEY = "${SUPABASE_PUBLISHABLE_KEY}"
                      local SECRET_KEY = "${SUPABASE_SECRET_KEY}"
                      local ANON_ROLE_JWT = "${SUPABASE_ANON_ROLE_JWT}"
                      local SERVICE_ROLE_JWT = "${SUPABASE_SERVICE_ROLE_JWT}"

                      local function protected_route(request_handle)
                        local route = request_handle:streamInfo():routeName()
                        return route == "auth-v1-protected"
                          or route == "rest-v1-openapi-protected"
                          or route == "rest-v1-protected"
                          or route == "pg-protected"
                      end

                      local function translate(key)
                        if key == PUBLISHABLE_KEY then
                          return ANON_ROLE_JWT
                        elseif key == SECRET_KEY then
                          return SERVICE_ROLE_JWT
                        end
                        return nil
                      end

                      local function replace_query_apikey(path, replacement)
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
                        if not protected_route(request_handle) then
                          return
                        end
                        local headers = request_handle:headers()
                        local path = headers:get(":path")
                        if path == nil then
                          return
                        end
                        local query_start = string.find(path, "?", 1, true)
                        if query_start == nil then
                          return
                        end
                        local query = string.sub(path, query_start + 1)
                        for key, value in string.gmatch(query, "([^&=]+)=([^&]*)") do
                          if key == "apikey" then
                            local translated = translate(value)
                            if translated ~= nil then
                              headers:replace("apikey", translated)
                              headers:replace(":path", replace_query_apikey(path, translated))
                            end
                            return
                          end
                        end
                      end

                # Translate a header apikey when the request did not use the
                # query form.
                - name: envoy.filters.http.lua
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
                    inline_code: |
                      local PUBLISHABLE_KEY = "${SUPABASE_PUBLISHABLE_KEY}"
                      local SECRET_KEY = "${SUPABASE_SECRET_KEY}"
                      local ANON_ROLE_JWT = "${SUPABASE_ANON_ROLE_JWT}"
                      local SERVICE_ROLE_JWT = "${SUPABASE_SERVICE_ROLE_JWT}"
                      function envoy_on_request(request_handle)
                        local headers = request_handle:headers()
                        local key = headers:get("apikey")
                        if key == PUBLISHABLE_KEY then
                          headers:replace("apikey", ANON_ROLE_JWT)
                        elseif key == SECRET_KEY then
                          headers:replace("apikey", SERVICE_ROLE_JWT)
                        end
                      end

                # Preserve a genuine user bearer JWT. Missing, non-Bearer, or
                # Bearer sb_* values are replaced with the translated role JWT.
                - name: envoy.filters.http.lua
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
                    inline_code: |
                      local function genuine_bearer(value)
                        if value == nil or string.sub(value, 1, 7) ~= "Bearer " then
                          return false
                        end
                        local token = string.sub(value, 8)
                        if token == "" or string.sub(token, 1, 3) == "sb_" then
                          return false
                        end
                        local header, claims, signature = string.match(token, "^([^%.]+)%.([^%.]+)%.([^%.]+)$")
                        return header ~= nil and claims ~= nil and signature ~= nil
                      end

                      function envoy_on_request(request_handle)
                        local route = request_handle:streamInfo():routeName()
                        if route ~= "auth-v1-protected" and route ~= "rest-v1-protected"
                          and route ~= "rest-v1-openapi-protected"
                          and route ~= "pg-protected" then
                          return
                        end
                        local headers = request_handle:headers()
                        if genuine_bearer(headers:get("authorization")) then
                          return
                        end
                        local key = headers:get("apikey")
                        if key ~= nil and key ~= "" then
                          headers:replace("authorization", "Bearer " .. key)
                        end
                      end

                # After translation, only the anon/service role JWTs are
                # accepted. The /pg/ route has a service-role-only policy.
                - name: envoy.filters.http.rbac
                  typed_config:
                    '@type': type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC
                    rules:
                      action: ALLOW
                      policies:
                        service-role:
                          permissions:
                            - url_path:
                                path:
                                  prefix: /pg/
                          principals:
                            - header:
                                name: apikey
                                string_match:
                                  exact: '${SUPABASE_SERVICE_ROLE_JWT}'
                        api:
                          permissions:
                            - url_path:
                                path:
                                  prefix: /auth/v1/
                            - url_path:
                                path:
                                  prefix: /rest/v1/
                          principals:
                            - header:
                                name: apikey
                                string_match:
                                  exact: '${SUPABASE_ANON_ROLE_JWT}'
                            - header:
                                name: apikey
                                string_match:
                                  exact: '${SUPABASE_SERVICE_ROLE_JWT}'
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
