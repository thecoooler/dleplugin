package manifest

import (
	"fmt"
	"regexp"
	"strings"
)

// phpIdent matches a PHP identifier; bytes 0x80-0xff are allowed like in PHP.
var phpIdent = regexp.MustCompile(`^[A-Za-z_\x80-\xff][A-Za-z0-9_\x80-\xff]*$`)

// ValidateNamespace accepts a PHP namespace without leading or trailing
// backslashes, e.g. Acme\ExamplePlugin.
func ValidateNamespace(ns string) error {
	for _, part := range strings.Split(ns, `\`) {
		if !phpIdent.MatchString(part) {
			return fmt.Errorf("invalid PHP namespace %q", ns)
		}
	}
	return nil
}

// NamespaceMount returns the mount directory the generated autoloader
// expects for ns: engine/ followed by the namespace as a path.
func NamespaceMount(ns string) string {
	return "engine/" + strings.ReplaceAll(ns, `\`, "/")
}

// namespaceFromMount derives the namespace from an engine/<A>/<B> mount;
// "" when the mount is not under engine/ or is not a valid namespace.
func namespaceFromMount(mount string) string {
	rest, ok := strings.CutPrefix(mount, "engine/")
	if !ok {
		return ""
	}
	ns := strings.ReplaceAll(rest, "/", `\`)
	if ValidateNamespace(ns) != nil {
		return ""
	}
	return ns
}

// resolveNamespace fills the defaults of namespace and mount and checks that
// the autoloader can find the plugin's classes. Without an explicit value
// the namespace comes from the mount, then from the slug; without an explicit
// mount a declared namespace sets it.
func (d *decoder) resolveNamespace(info *Info, explicit bool) {
	if !explicit {
		info.Namespace = namespaceFromMount(info.Mount)
		if info.Namespace == "" && ValidateNamespace(info.Slug) == nil {
			info.Namespace = info.Slug
		}
	} else if info.Mount == "" {
		info.Mount = NamespaceMount(info.Namespace)
	}
	if !info.Extras.Autoloader[0] && !info.Extras.Autoloader[1] {
		return
	}
	switch {
	case info.Namespace == "":
		d.fail("extras.autoloader", "namespace is required: set namespace or mount: engine/<Vendor>/<Name>")
	case info.Mount != "" && info.Mount != NamespaceMount(info.Namespace):
		d.fail("extras.autoloader", fmt.Sprintf("mount must be %s to autoload namespace %s",
			NamespaceMount(info.Namespace), info.Namespace))
	}
}
