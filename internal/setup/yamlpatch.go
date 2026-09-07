// Package setup implements `quietdmd setup`: it automates the self-hosting
// walkthrough in docs/self-host.md and docs/matrix-setup.md end to end, apart
// from the one secret only the user has (their Instagram login).
package setup

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PatchYAMLFile reads path, hands the top-level mapping node to patch, and
// writes the result back with the file's existing permissions.
//
// This edits the node tree rather than unmarshalling into a struct and
// marshalling it back, because Synapse's and the bridge's generated YAML is
// heavily commented and carries keys this package does not know about;
// round-tripping through a struct would silently drop all of that. Only the
// keys patch actually touches change.
func PatchYAMLFile(path string, patch func(root *yaml.Node) error) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("parse %s: not a YAML mapping document", path)
	}
	if err := patch(doc.Content[0]); err != nil {
		return fmt.Errorf("patch %s: %w", path, err)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(path, out, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// FindKey walks a mapping node through path, returning the node at the end,
// or nil if any segment is missing or not a mapping.
func FindKey(mapNode *yaml.Node, path ...string) *yaml.Node {
	node := mapNode
	for _, key := range path {
		if node == nil || node.Kind != yaml.MappingNode {
			return nil
		}
		node = mapValue(node, key)
	}
	return node
}

// GetString reads a scalar string at path, reporting whether it was present.
func GetString(mapNode *yaml.Node, path ...string) (string, bool) {
	n := FindKey(mapNode, path...)
	if n == nil || n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Value, true
}

// GetBool reads a scalar bool at path, reporting whether it was present.
func GetBool(mapNode *yaml.Node, path ...string) (bool, bool) {
	s, ok := GetString(mapNode, path...)
	if !ok {
		return false, false
	}
	return s == "true", true
}

// SetPath sets a nested key to value, creating intermediate mappings as
// needed. path must have at least one element.
func SetPath(mapNode *yaml.Node, value *yaml.Node, path ...string) error {
	if len(path) == 0 {
		return fmt.Errorf("yamlpatch: empty path")
	}
	node := mapNode
	for _, key := range path[:len(path)-1] {
		next := mapValue(node, key)
		if next == nil {
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMapKey(node, key, next)
		} else if next.Kind != yaml.MappingNode {
			return fmt.Errorf("yamlpatch: %s is not a mapping", strings.Join(path, "."))
		}
		node = next
	}
	setMapKey(node, path[len(path)-1], value)
	return nil
}

func mapValue(mapNode *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		if mapNode.Content[i].Value == key {
			return mapNode.Content[i+1]
		}
	}
	return nil
}

func setMapKey(mapNode *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		if mapNode.Content[i].Value == key {
			mapNode.Content[i+1] = value
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	mapNode.Content = append(mapNode.Content, keyNode, value)
}

// Str, Bool and StrList build scalar/sequence nodes for use with SetPath.
func Str(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func Bool(b bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(b)}
}

func StrList(items ...string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, it := range items {
		n.Content = append(n.Content, Str(it))
	}
	return n
}
