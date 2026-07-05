package integrate

// mergeNodes returns a merged tree of dst and src. On any collision (differing kinds,
// scalar-vs-scalar, or scalar within a sequence), the side named by preferSrc wins.
//
// Mapping merge is deep: keys present on both sides recurse; keys present on only one
// side are preserved. Output key order is [preferred-side keys in preferred order,
// followed by other-side-only keys in their original order].
//
// Sequence merge policy depends on element kinds:
//   - all elements on both sides are scalars → concat + dedupe (preferred first)
//   - anything else → wholesale replace with the preferred side
//
// Nil handling: if one side is nil the other side is returned as-is; if both are nil,
// nil is returned.
func mergeNodes(dst, src *node, preferSrc bool) *node {
	if dst == nil {
		return src
	}
	if src == nil {
		return dst
	}

	preferred, other := dst, src
	if preferSrc {
		preferred, other = src, dst
	}

	if preferred.kind != other.kind {
		return preferred
	}

	switch preferred.kind {
	case nodeScalar:
		return preferred
	case nodeMapping:
		return mergeMappings(preferred, other, preferSrc)
	case nodeSequence:
		return mergeSequences(preferred, other)
	}
	return preferred
}

func mergeMappings(preferred, other *node, preferSrc bool) *node {
	result := newMappingNode()
	// preferred-side keys first (in preferred order), recursing on collisions
	for _, k := range preferred.mapping.keys {
		pv := preferred.mapping.values[k]
		if ov, ok := other.mapping.Get(k); ok {
			// call mergeNodes with the original dst/src orientation so nested merges honor preferSrc
			var dst, src *node
			if preferSrc {
				dst, src = ov, pv
			} else {
				dst, src = pv, ov
			}
			result.mapping.Set(k, mergeNodes(dst, src, preferSrc))
		} else {
			result.mapping.Set(k, pv)
		}
	}
	// other-side-only keys appended in their original order
	for _, k := range other.mapping.keys {
		if _, ok := preferred.mapping.Get(k); ok {
			continue
		}
		result.mapping.Set(k, other.mapping.values[k])
	}
	return result
}

func mergeSequences(preferred, other *node) *node {
	if !allScalars(preferred) || !allScalars(other) {
		return preferred
	}
	result := newSequenceNode()
	seen := make(map[any]struct{}, len(preferred.seq)+len(other.seq))
	appendUnique := func(items []*node) {
		for _, item := range items {
			if _, dup := seen[item.scalar]; dup {
				continue
			}
			seen[item.scalar] = struct{}{}
			result.seq = append(result.seq, item)
		}
	}
	appendUnique(preferred.seq)
	appendUnique(other.seq)
	return result
}

func allScalars(n *node) bool {
	for _, item := range n.seq {
		if item.kind != nodeScalar {
			return false
		}
	}
	return true
}
