package converter

// resourceIndex contains only root-module resources that this converter emits.
// A reference to any other resource must not become a dangling intrinsic.
type resourceIndex struct {
	byAddress map[string]*plannedResource
	byLogical map[string]map[string]int
	byARN     map[string][]*plannedResource
}

func newResourceIndex(resources []plannedResource) *resourceIndex {
	index := &resourceIndex{
		byAddress: make(map[string]*plannedResource),
		byLogical: make(map[string]map[string]int),
		byARN:     make(map[string][]*plannedResource),
	}
	for i := range resources {
		r := &resources[i]
		if r.Mode != "managed" || !supportedResourceType(r.Type) {
			continue
		}
		index.byAddress[r.Address] = r
		if index.byLogical[r.Type] == nil {
			index.byLogical[r.Type] = make(map[string]int)
		}
		index.byLogical[r.Type][resourceLogicalID(*r)]++
		if r.Type == "aws_cloudfront_function" {
			if arn := str(r.Values, "arn"); arn != "" {
				index.byARN[arn] = append(index.byARN[arn], r)
			}
		}
	}
	return index
}

func supportedResourceType(typ string) bool {
	if typ == "aws_cloudfront_distribution" {
		return true
	}
	_, ok := converters[typ]
	return ok
}

func (index *resourceIndex) findManagedResource(address, typ string) *plannedResource {
	r := index.byAddress[address]
	if r == nil || r.Type != typ || index.byLogical[typ][resourceLogicalID(*r)] != 1 {
		return nil
	}
	return r
}

func (index *resourceIndex) hasLogicalID(typ, id string) bool {
	return index.byLogical[typ][id] == 1
}

func (index *resourceIndex) functionByARN(arn string) (*plannedResource, bool) {
	matches := index.byARN[arn]
	if len(matches) == 0 {
		return nil, false
	}
	if len(matches) != 1 || index.byLogical["aws_cloudfront_function"][resourceLogicalID(*matches[0])] != 1 {
		return nil, true
	}
	return matches[0], false
}
