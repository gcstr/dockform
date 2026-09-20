package planner

import (
	"strings"

	"github.com/gcstr/dockform/internal/dockercli"
)

// containerResolver maps the names compose reports in progress events back to
// the stack's services. A progress event's id is a container name, never a
// service name, and with container_name set the service does not appear in it
// at all, so the mapping comes from the stack's own compose config.
//
// The zero value resolves nothing, which is how a stack whose config could not
// be read degrades to stack-level display.
type containerResolver struct {
	explicit map[string]string   // container_name -> service
	prefixes map[string]string   // "<project>-<service>-" -> service
	images   map[string][]string // image ref -> services using it, sorted
}

func newContainerResolver(doc dockercli.ComposeConfigDoc, project string) containerResolver {
	r := containerResolver{
		explicit: map[string]string{},
		prefixes: map[string]string{},
		images:   map[string][]string{},
	}
	for _, name := range sortedKeys(doc.Services) {
		svc := doc.Services[name]
		if svc.ContainerName != "" {
			r.explicit[svc.ContainerName] = name
		} else if project != "" {
			r.prefixes[project+"-"+name+"-"] = name
		}
		if svc.Image != "" {
			r.images[svc.Image] = append(r.images[svc.Image], name)
		}
	}
	return r
}

// service resolves a container name. An explicit container_name wins.
// Otherwise compose names a container "<project>-<service>-<N>", matched as that
// prefix followed by digits only. That is unambiguous: if one prefix extends
// another, the shorter prefix leaves a remainder containing "-", which is not
// all digits — for project "a", "a-web-api-1" is "web-api", never "web".
func (r containerResolver) service(container string) (string, bool) {
	if svc, ok := r.explicit[container]; ok {
		return svc, true
	}
	for prefix, svc := range r.prefixes {
		if rest, ok := strings.CutPrefix(container, prefix); ok && rest != "" && allDigits(rest) {
			return svc, true
		}
	}
	return "", false
}

// servicesForImage returns the stack's services that use image, sorted. Image
// refs match exactly: compose neither normalises the ref in its config nor in
// its progress events (see pull_fully_qualified_ref.jsonl).
func (r containerResolver) servicesForImage(image string) []string {
	return r.images[image]
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
