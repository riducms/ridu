package store

import "encoding/json"

const valueListBits = 5
const valueListWidth = 1 << valueListBits

// valueList is an immutable radix tree. Its shape depends only on list length:
// construction, decoding and item replacement therefore remain DeepEqual when
// their values are equal. A node is either a leaf or a branch; even a partial
// rightmost branch retains its level. Empty lists use a nil root.
type valueList struct {
	items    []Value
	children []*valueList
	length   int
}

func newValueList(items []Value) *valueList {
	if len(items) == 0 {
		return nil
	}
	return buildValueList(items, valueListShift(len(items)))
}

func valueListShift(length int) uint {
	var shift uint
	for remaining := length - 1; remaining >= valueListWidth; remaining >>= valueListBits {
		shift += valueListBits
	}
	return shift
}

func buildValueList(items []Value, shift uint) *valueList {
	if shift == 0 {
		return &valueList{items: cloneValueList(items), length: len(items)}
	}
	span := 1 << shift
	children := make([]*valueList, (len(items)-1)/span+1)
	for index := range children {
		start := index * span
		children[index] = buildValueList(items[start:min(start+span, len(items))], shift-valueListBits)
	}
	return &valueList{children: children, length: len(items)}
}

func (list *valueList) len() int {
	if list == nil {
		return 0
	}
	return list.length
}

func (list *valueList) item(index int, shift uint) Value {
	if shift == 0 {
		return list.items[index]
	}
	return list.children[index>>shift].item(index&((1<<shift)-1), shift-valueListBits)
}

func (list *valueList) withItem(index int, replacement Value, shift uint) *valueList {
	if shift == 0 {
		items := cloneValueList(list.items)
		items[index] = replacement
		return &valueList{items: items, length: list.length}
	}
	children := append([]*valueList(nil), list.children...)
	child := index >> shift
	children[child] = children[child].withItem(index&((1<<shift)-1), replacement, shift-valueListBits)
	return &valueList{children: children, length: list.length}
}

func (list *valueList) copyTo(destination []Value) int {
	if list == nil {
		return 0
	}
	if list.children == nil {
		return copy(destination, list.items)
	}
	copied := 0
	for _, child := range list.children {
		copied += child.copyTo(destination[copied:])
	}
	return copied
}

// visit stops immediately when a visitor returns false. Population budgets can
// reject a response without first materializing its entire list.
func (list *valueList) visit(visitor func(Value) bool) bool {
	if list == nil {
		return true
	}
	if list.children == nil {
		for _, item := range list.items {
			if !visitor(item) {
				return false
			}
		}
		return true
	}
	for _, child := range list.children {
		if !child.visit(visitor) {
			return false
		}
	}
	return true
}

func (list *valueList) marshalJSON() ([]byte, error) {
	encoded := []byte{'['}
	var marshalError error
	list.visit(func(item Value) bool {
		// Marshal each Value through encoding/json, as slice encoding does, so
		// unsupported child values retain their MarshalerError wrapping.
		itemJSON, err := json.Marshal(item)
		if err != nil {
			marshalError = err
			return false
		}
		if len(encoded) > 1 {
			encoded = append(encoded, ',')
		}
		encoded = append(encoded, itemJSON...)
		return true
	})
	if marshalError != nil {
		return nil, marshalError
	}
	return append(encoded, ']'), nil
}
