package planner

import (
	"reflect"
	"testing"

	"github.com/gcstr/dockform/internal/dockercli"
)

func resolverFor(project string, services map[string]dockercli.ComposeService) containerResolver {
	return newContainerResolver(dockercli.ComposeConfigDoc{Name: project, Services: services}, project)
}

// With container_name set, compose's event carries no trace of the service.
func TestContainerResolver_ExplicitContainerName(t *testing.T) {
	r := resolverFor("skqcn", map[string]dockercli.ComposeService{
		"gamma": {ContainerName: "totally-custom-name"},
	})
	if svc, ok := r.service("totally-custom-name"); !ok || svc != "gamma" {
		t.Fatalf("got %q, %v; want gamma, true", svc, ok)
	}
}

func TestContainerResolver_DefaultName(t *testing.T) {
	r := resolverFor("skqfixture", map[string]dockercli.ComposeService{"alpha": {}})
	if svc, ok := r.service("skqfixture-alpha-1"); !ok || svc != "alpha" {
		t.Fatalf("got %q, %v; want alpha, true", svc, ok)
	}
	if svc, ok := r.service("skqfixture-alpha-12"); !ok || svc != "alpha" {
		t.Fatalf("replica 12: got %q, %v", svc, ok)
	}
}

// "a-web-api-1" leaves "api-1" after the "a-web-" prefix, which is not all
// digits, so it must not be mistaken for service "web".
func TestContainerResolver_ServiceNamesThatPrefixEachOther(t *testing.T) {
	r := resolverFor("a", map[string]dockercli.ComposeService{"web": {}, "web-api": {}})
	for container, want := range map[string]string{"a-web-1": "web", "a-web-api-1": "web-api"} {
		if svc, ok := r.service(container); !ok || svc != want {
			t.Errorf("%s: got %q, %v; want %q", container, svc, ok, want)
		}
	}
}

func TestContainerResolver_ExplicitNameWins(t *testing.T) {
	r := resolverFor("a", map[string]dockercli.ComposeService{
		"web":   {},
		"other": {ContainerName: "a-web-1"},
	})
	if svc, _ := r.service("a-web-1"); svc != "other" {
		t.Fatalf("got %q, want other", svc)
	}
}

func TestContainerResolver_Unmappable(t *testing.T) {
	r := resolverFor("a", map[string]dockercli.ComposeService{"web": {}})
	for _, c := range []string{"b-web-1", "a-web-", "a-web-x", "a-db-1", ""} {
		if svc, ok := r.service(c); ok {
			t.Errorf("%q resolved to %q; want unmappable", c, svc)
		}
	}
}

func TestContainerResolver_ZeroValueResolvesNothing(t *testing.T) {
	var r containerResolver
	if _, ok := r.service("a-web-1"); ok {
		t.Fatal("zero resolver resolved a container")
	}
	if got := r.servicesForImage("alpine:3.22"); got != nil {
		t.Fatalf("zero resolver returned %v", got)
	}
}

func TestContainerResolver_ServicesSharingAnImage(t *testing.T) {
	r := resolverFor("a", map[string]dockercli.ComposeService{
		"worker": {Image: "app:1"},
		"api":    {Image: "app:1"},
		"db":     {Image: "postgres:16"},
	})
	if got, want := r.servicesForImage("app:1"), []string{"api", "worker"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
