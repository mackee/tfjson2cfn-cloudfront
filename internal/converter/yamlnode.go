package converter

import (
	"math"

	"gopkg.in/yaml.v3"
)

// This file builds the CloudFormation output as a *yaml.Node tree. Working at
// the node level (rather than marshalling Go maps) lets us emit CloudFormation
// short-form intrinsics (!Ref, !GetAtt) and keep a deliberate key order.

// mapping accumulates key/value pairs in insertion order, skipping any pair
// whose value node is nil. Omitting a property is therefore just passing nil.
type mapping struct {
	node *yaml.Node
}

func newMapping() *mapping {
	return &mapping{node: &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}}
}

// set appends key: value. A nil value is skipped, so callers express "omit this
// property when it is at its default" by handing in nil.
func (m *mapping) set(key string, value *yaml.Node) *mapping {
	if value == nil {
		return m
	}
	m.node.Content = append(m.node.Content, scalar(key), value)
	return m
}

// empty reports whether the mapping has no entries.
func (m *mapping) empty() bool { return len(m.node.Content) == 0 }

// orNil returns the mapping node, or nil when empty (so empty optional blocks
// are omitted rather than emitted as `{}`). Use node() when an explicit empty
// mapping is wanted (e.g. S3OriginConfig: {}).
func (m *mapping) orNil() *yaml.Node {
	if m.empty() {
		return nil
	}
	return m.node
}

func (m *mapping) emptyNode() *yaml.Node { return m.node }

// sequence builds a sequence node from the given items, skipping nil items.
// Returns nil when no items remain, so empty lists are omitted.
func sequence(items ...*yaml.Node) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, it := range items {
		if it != nil {
			seq.Content = append(seq.Content, it)
		}
	}
	if len(seq.Content) == 0 {
		return nil
	}
	return seq
}

// scalar builds a scalar node for a Go value. Integral float64 values (as
// produced by encoding/json) are emitted as integers so ports and status codes
// render as `3000`, not `3000.0`.
func scalar(v any) *yaml.Node {
	if f, ok := v.(float64); ok && f == math.Trunc(f) && !math.IsInf(f, 0) &&
		f >= math.MinInt64 && f <= math.MaxInt64 {
		v = int64(f)
	}
	n := &yaml.Node{}
	_ = n.Encode(v)
	return n
}

// literalScalar emits a string using YAML literal block style (`|`), suited to
// multi-line CloudFront function code.
func literalScalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s, Style: yaml.LiteralStyle}
}

// refNode emits `!Ref LogicalID`.
func refNode(logicalID string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!Ref", Value: logicalID}
}

// getAttNode emits `!GetAtt LogicalID.Attribute`.
func getAttNode(logicalID, attr string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!GetAtt", Value: logicalID + "." + attr}
}
