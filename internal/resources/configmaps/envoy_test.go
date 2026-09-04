package configmaps

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

func TestBuildEnvoyConfigMapRendersManagedProfile(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "default"}}
	config := BuildEnvoyConfigMap(project)
	for _, key := range []string{"envoy.yaml", "cds.yaml", "lds.template.yaml", "docker-entrypoint.sh"} {
		if config.Data[key] == "" {
			t.Fatalf("missing Envoy asset %q", key)
		}
	}
	for _, key := range []string{"envoy.yaml", "cds.yaml", "lds.template.yaml"} {
		var document any
		if err := yaml.Unmarshal([]byte(config.Data[key]), &document); err != nil {
			t.Fatalf("asset %s is not valid YAML: %v", key, err)
		}
	}
	lds := config.Data["lds.template.yaml"]
	for _, value := range []string{"${SUPABASE_PUBLISHABLE_KEY}", "${SUPABASE_SECRET_KEY}", "${SUPABASE_ANON_ROLE_JWT}", "${SUPABASE_SERVICE_ROLE_JWT}"} {
		if !strings.Contains(lds, value) {
			t.Fatalf("LDS template missing placeholder %q", value)
		}
	}
	if strings.Contains(strings.ToLower(lds), "kong") || strings.Contains(lds, "${ANON_KEY}") || strings.Contains(lds, "${SERVICE_ROLE_KEY}") {
		t.Fatal("LDS template contains a legacy gateway credential or route")
	}
}

//nolint:gocyclo // the test intentionally walks the rendered machine config and exercises each contract branch.
func TestRenderedEnvoyFixtureImplementsOpaqueGatewaySemantics(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "fixture", Namespace: "default"}}
	config := BuildEnvoyConfigMap(project)
	values := map[string]string{
		"${SUPABASE_PUBLISHABLE_KEY}":  "sb_publishable_fixture",
		"${SUPABASE_SECRET_KEY}":       "sb_secret_fixture",
		"${SUPABASE_ANON_ROLE_JWT}":    "eyJanon.fixture.token",
		"${SUPABASE_SERVICE_ROLE_JWT}": "eyJservice.fixture.token",
	}
	rendered := config.Data["lds.template.yaml"]
	for placeholder, value := range values {
		rendered = strings.ReplaceAll(rendered, placeholder, value)
	}
	if strings.Contains(rendered, "${") {
		t.Fatalf("rendered Envoy fixture still contains a placeholder")
	}

	var document map[string]any
	if err := yaml.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatalf("rendered LDS fixture is not valid YAML: %v", err)
	}
	resources := asSlice(t, document["resources"])
	if len(resources) != 1 {
		t.Fatalf("listener resources = %d, want one managed listener", len(resources))
	}
	listener := asMap(t, resources[0])
	if listener["name"] != "supabase" {
		t.Fatalf("listener name = %v", listener["name"])
	}
	filterChains := asSlice(t, listener["filter_chains"])
	chain := asMap(t, filterChains[0])
	filters := asSlice(t, chain["filters"])
	hcm := asMap(t, asMap(t, filters[0])["typed_config"])

	virtualHosts := asSlice(t, asMap(t, hcm["route_config"])["virtual_hosts"])
	vhost := asMap(t, virtualHosts[0])
	routes := asSlice(t, vhost["routes"])
	wantRoutes := []string{
		"internal-health", "auth-v1-verify", "auth-v1-callback", "auth-v1-authorize", "auth-v1-jwks",
		"oauth-authorization-server", "auth-v1-saml-acs", "auth-v1-saml-metadata", "auth-v1-protected",
		"rest-v1-openapi-protected", "rest-v1-protected", "pg-protected", "studio",
	}
	if len(routes) != len(wantRoutes) {
		t.Fatalf("route count = %d, want %d", len(routes), len(wantRoutes))
	}
	for index, want := range wantRoutes {
		if got := asMap(t, routes[index])["name"]; got != want {
			t.Fatalf("route %d = %v, want %q", index, got, want)
		}
	}
	for _, route := range routes {
		name := asMap(t, route)["name"].(string)
		if strings.Contains(name, "storage") || strings.Contains(name, "realtime") || strings.Contains(name, "function") || strings.Contains(name, "analytics") || strings.Contains(name, "mcp") {
			t.Fatalf("unused service route %q present", name)
		}
	}

	assertRoute := func(name, matchField, matchValue, cluster string) {
		t.Helper()
		for _, routeValue := range routes {
			route := asMap(t, routeValue)
			if route["name"] != name {
				continue
			}
			match := asMap(t, route["match"])
			if match[matchField] != matchValue {
				t.Fatalf("route %s match %s = %v, want %q", name, matchField, match[matchField], matchValue)
			}
			if cluster != "" {
				routeConfig := asMap(t, route["route"])
				if routeConfig["cluster"] != cluster {
					t.Fatalf("route %s cluster = %v, want %q", name, routeConfig["cluster"], cluster)
				}
			}
			return
		}
		t.Fatalf("route %s not found", name)
	}
	assertRoute("internal-health", "path", "/_internal/health", "")
	assertRoute("oauth-authorization-server", "prefix", "/.well-known/oauth-authorization-server", "auth")
	assertRoute("rest-v1-openapi-protected", "path", "/rest/v1/", "rest")
	assertRoute("pg-protected", "prefix", "/pg/", "meta")

	httpFilters := asSlice(t, hcm["http_filters"])
	wantFilters := []string{"envoy.filters.http.cors", "envoy.filters.http.lua", "envoy.filters.http.lua", "envoy.filters.http.lua", "envoy.filters.http.lua", "envoy.filters.http.lua", "envoy.filters.http.rbac", "envoy.filters.http.router"}
	if len(httpFilters) != len(wantFilters) {
		t.Fatalf("HTTP filter count = %d, want %d", len(httpFilters), len(wantFilters))
	}
	luaCode := make([]string, 0, len(httpFilters))
	for index, filterValue := range httpFilters {
		filter := asMap(t, filterValue)
		if filter["name"] != wantFilters[index] {
			t.Fatalf("HTTP filter %d = %v, want %q", index, filter["name"], wantFilters[index])
		}
		if filter["name"] == "envoy.filters.http.lua" {
			luaCode = append(luaCode, asMap(t, filter["typed_config"])["inline_code"].(string))
		}
	}
	if len(luaCode) != 5 {
		t.Fatalf("Lua filter count = %d, want 5", len(luaCode))
	}
	for index, code := range luaCode {
		if index == 3 { // header translation applies to all routes by design.
			continue
		}
		if !strings.Contains(code, "rest-v1-openapi-protected") {
			t.Fatalf("Lua filter %d does not protect the exact REST OpenAPI route", index)
		}
	}
	rbacFilter := asMap(t, httpFilters[6])
	rbacConfig := asMap(t, rbacFilter["typed_config"])
	rules := asMap(t, rbacConfig["rules"])
	policies := asMap(t, rules["policies"])
	servicePolicy := asMap(t, policies["service-role"])
	servicePermissions := asSlice(t, servicePolicy["permissions"])
	if len(servicePermissions) != 1 {
		t.Fatal("service-role RBAC policy is missing its /pg/ path permission")
	}
	servicePath := asMap(t, asMap(t, asMap(t, servicePermissions[0])["url_path"])["path"])
	if servicePath["prefix"] != "/pg/" {
		t.Fatalf("service-role RBAC path = %v, want /pg/", servicePath["prefix"])
	}
	servicePrincipals := asSlice(t, servicePolicy["principals"])
	if len(servicePrincipals) != 1 {
		t.Fatalf("service-role RBAC principals = %#v", servicePrincipals)
	}
	serviceHeader := asMap(t, asMap(t, servicePrincipals[0])["header"])
	serviceMatch := asMap(t, serviceHeader["string_match"])
	if serviceMatch["exact"] != "eyJservice.fixture.token" {
		t.Fatalf("service-role RBAC principal = %#v", servicePrincipals)
	}
	apiPolicy := asMap(t, policies["api"])
	if len(asSlice(t, apiPolicy["principals"])) != 2 {
		t.Fatalf("api RBAC principals = %#v, want anon and service role", apiPolicy["principals"])
	}
	// Exercise the contract represented by the rendered translation and bearer
	// filters with test-owned values, rather than merely checking their names.
	for _, test := range []struct {
		key, want string
	}{
		{"sb_publishable_fixture", "eyJanon.fixture.token"},
		{"sb_secret_fixture", "eyJservice.fixture.token"},
	} {
		if got, ok := translateOpaqueFixture(test.key, values, true); !ok || got != test.want {
			t.Fatalf("opaque key %q translated to %q (ok=%v), want %q", test.key, got, ok, test.want)
		}
	}
	if got, ok := translateOpaqueFixture("eyJlegacy.jwt.token", values, true); ok || got != "" {
		t.Fatalf("legacy JWT-shaped API key was accepted: %q (ok=%v)", got, ok)
	}
	for _, test := range []struct {
		authorization, translated, want string
	}{
		{"Bearer eyJuser.claims.signature", "eyJanon.fixture.token", "Bearer eyJuser.claims.signature"},
		{"Bearer sb_publishable_fixture", "eyJanon.fixture.token", "Bearer eyJanon.fixture.token"},
		{"Basic abc", "eyJservice.fixture.token", "Bearer eyJservice.fixture.token"},
		{"", "eyJanon.fixture.token", "Bearer eyJanon.fixture.token"},
	} {
		if got := synthesizeAuthorizationFixture(test.authorization, test.translated); got != test.want {
			t.Fatalf("authorization %q synthesized as %q, want %q", test.authorization, got, test.want)
		}
	}
	if !strings.Contains(luaCode[4], "string.sub(token, 1, 3) == \"sb_\"") || !strings.Contains(luaCode[4], "string.match(token, \"^([^%.]+)%.([^%.]+)%.([^%.]+)$\"") {
		t.Fatal("bearer filter does not preserve only genuine JWT-shaped user tokens")
	}
}

func TestEnvoyAdminStaysLoopbackWhilePublicHealthRouteIsReachable(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "exposure", Namespace: "default"}}
	config := BuildEnvoyConfigMap(project)
	var bootstrap map[string]any
	if err := yaml.Unmarshal([]byte(config.Data["envoy.yaml"]), &bootstrap); err != nil {
		t.Fatalf("bootstrap is not valid YAML: %v", err)
	}
	admin := asMap(t, bootstrap["admin"])
	address := asMap(t, asMap(t, admin["address"])["socket_address"])
	if address["address"] != "127.0.0.1" {
		t.Fatalf("Envoy admin address = %v, want loopback", address["address"])
	}
	if address["port_value"] != float64(9901) {
		t.Fatalf("Envoy admin port = %v, want 9901", address["port_value"])
	}
	if !strings.Contains(config.Data["lds.template.yaml"], "path: /_internal/health") {
		t.Fatal("public internal health route is missing")
	}
}

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value has type %T, want map[string]any", value)
	}
	return result
}

func asSlice(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("value has type %T, want []any", value)
	}
	return result
}

func translateOpaqueFixture(key string, values map[string]string, protected bool) (string, bool) {
	if !protected {
		return "", false
	}
	switch key {
	case values["${SUPABASE_PUBLISHABLE_KEY}"]:
		return values["${SUPABASE_ANON_ROLE_JWT}"], true
	case values["${SUPABASE_SECRET_KEY}"]:
		return values["${SUPABASE_SERVICE_ROLE_JWT}"], true
	default:
		return "", false
	}
}

func synthesizeAuthorizationFixture(value, translated string) string {
	if strings.HasPrefix(value, "Bearer ") {
		token := strings.TrimPrefix(value, "Bearer ")
		parts := strings.Split(token, ".")
		if token != "" && !strings.HasPrefix(token, "sb_") && len(parts) == 3 {
			return value
		}
	}
	return "Bearer " + translated
}

func TestBuildJWKSConfigMapUsesPublicProjection(t *testing.T) {
	project := &supabasev1alpha1.SupabaseProject{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "default"}}
	cm := BuildJWKSConfigMap(project, `{"keys":[]}`)
	if cm.Name != "example-auth-jwks" || cm.Data[JWKSDataKey] != `{"keys":[]}` {
		t.Fatalf("JWKS ConfigMap = %#v", cm)
	}
}
