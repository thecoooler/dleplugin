// Package manifest holds the dleplugin.yml model and its YAML decoding.
// JSON tags define the normalized form compared with testdata/golden.
package manifest

// LifecycleHooks lists lifecycle hook names in XML/DB emission order.
var LifecycleHooks = []string{"install", "upgrade", "enable", "disable", "delete"}

// Action is a single DLE file patch operation.
type Action struct {
	Name    string `json:"name"`
	Search  string `json:"search"`
	Replace string `json:"replace"`
}

// FilePatch is the ordered list of operations applied to one DLE file.
type FilePatch struct {
	Name    string   `json:"name"`
	Actions []Action `json:"actions"`
}

// Lifecycle holds SQL or PHP lifecycle hook bodies.
type Lifecycle struct {
	Install string `json:"install"`
	Upgrade string `json:"upgrade"`
	Enable  string `json:"enable"`
	Disable string `json:"disable"`
	Delete  string `json:"delete"`
}

// Get returns the hook body by name (one of LifecycleHooks).
func (l Lifecycle) Get(hook string) string {
	switch hook {
	case "install":
		return l.Install
	case "upgrade":
		return l.Upgrade
	case "enable":
		return l.Enable
	case "disable":
		return l.Disable
	case "delete":
		return l.Delete
	}
	return ""
}

// Extras holds generated helper edits.
type Extras struct {
	AdminSection bool `json:"admin_section"`
	// Autoloader toggles injection into DLEFunctionFiles by index.
	Autoloader [2]bool  `json:"autoloader"`
	Configs    []string `json:"configs"`
}

// BuildOptions holds build options.
type BuildOptions struct {
	Split bool `json:"split"`
}

// Info is the normalized dleplugin.yml manifest.
type Info struct {
	Files       *OrderedMap[*FilePatch] `json:"files"`
	Slug        string                  `json:"slug"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	// Mount is a normalized slash-separated path ("" when unset).
	Mount string `json:"mount"`
	// Namespace is the PHP namespace of the plugin classes ("" when it cannot
	// be derived); the autoloader resolves it under the mount.
	Namespace string `json:"namespace"`
	// Dependencies are names of plugins required by this one (DLE's
	// needplugin).
	Dependencies []string     `json:"dependencies,omitempty"`
	Build        BuildOptions `json:"build"`
	Extras       Extras       `json:"extras"`
	// Alias maps a normalized path relative to src/ to a normalized target.
	Alias *OrderedMap[string] `json:"alias"`
	MySQL Lifecycle           `json:"mysql"`
	PHP   Lifecycle           `json:"php"`
}

// NewInfo returns an Info with every collection initialized and defaults set.
func NewInfo() *Info {
	return &Info{
		Files:  NewOrderedMap[*FilePatch](),
		Extras: Extras{Configs: []string{}},
		Alias:  NewOrderedMap[string](),
	}
}

// AppendActions appends actions to the patch for file, creating it at the end
// of the file order when missing.
func (i *Info) AppendActions(file string, actions ...Action) {
	if patch, ok := i.Files.Get(file); ok {
		patch.Actions = append(patch.Actions, actions...)
		return
	}
	i.Files.Set(file, &FilePatch{Name: file, Actions: append([]Action{}, actions...)})
}

// Clone returns a deep copy.
func (i *Info) Clone() *Info {
	c := *i
	c.Files = NewOrderedMap[*FilePatch]()
	for _, name := range i.Files.Keys() {
		patch, _ := i.Files.Get(name)
		c.Files.Set(name, &FilePatch{Name: patch.Name, Actions: append([]Action{}, patch.Actions...)})
	}
	c.Extras.Configs = append([]string{}, i.Extras.Configs...)
	if i.Dependencies != nil {
		c.Dependencies = append([]string{}, i.Dependencies...)
	}
	c.Alias = i.Alias.Clone()
	return &c
}

// SourceFile is a file under the plugin src/ directory and its DLE target.
type SourceFile struct {
	// Src is the absolute path on disk.
	Src string `json:"src"`
	// Mount is the slash-separated target path relative to the DLE root.
	Mount string `json:"mount"`
}
