package types

import (
	"reflect"
)

type basicSliceSSZ struct{}

func newBasicSliceSSZ() *basicSliceSSZ {
	return &basicSliceSSZ{}
}

func (b *basicSliceSSZ) Marshal(val reflect.Value, typ reflect.Type, buf []byte, startOffset uint64) (uint64, error) {
	index := startOffset
	var err error
	if val.Len() == 0 {
		return index, nil
	}
	factory, err := SSZFactory(val.Index(0), typ.Elem())
	if err != nil {
		return 0, err
	}
	for i := 0; i < val.Len(); i++ {
		index, err = factory.Marshal(val.Index(i), typ.Elem(), buf, index)
		if err != nil {
			return 0, err
		}
	}
	return index, nil
}

func (b *basicSliceSSZ) Unmarshal(val reflect.Value, typ reflect.Type, input []byte, startOffset uint64) (uint64, error) {
	if len(input) == 0 {
		newVal := reflect.MakeSlice(val.Type(), 0, 0)
		val.Set(newVal)
		return 0, nil
	}
	// If there are struct tags that specify a different type, we handle accordingly.
	if val.Type() != typ {
		sizes := []uint64{1}
		innerElement := typ.Elem()
		for {
			if innerElement.Kind() == reflect.Slice {
				sizes = append(sizes, 0)
				innerElement = innerElement.Elem()
			} else if innerElement.Kind() == reflect.Array {
				sizes = append(sizes, uint64(innerElement.Len()))
				innerElement = innerElement.Elem()
			} else {
				break
			}
		}
		// If the item is a slice, we grow it accordingly based on the size tags.
		result := growSliceFromSizeTags(val, sizes)
		reflect.Copy(result, val)
		val.Set(result)
	} else {
		growConcreteSliceType(val, val.Type(), 1)
	}

	var err error
	index := startOffset
	factory, err := SSZFactory(val.Index(0), typ.Elem())
	if err != nil {
		return 0, err
	}
	index, err = factory.Unmarshal(val.Index(0), typ.Elem(), input, index)
	if err != nil {
		return 0, err
	}

	elementSize := index - startOffset
	endOffset := uint64(len(input)) / elementSize
	if val.Type() != typ {
		sizes := []uint64{endOffset}
		innerElement := typ.Elem()
		for {
			if innerElement.Kind() == reflect.Slice {
				sizes = append(sizes, 0)
				innerElement = innerElement.Elem()
			} else if innerElement.Kind() == reflect.Array {
				sizes = append(sizes, uint64(innerElement.Len()))
				innerElement = innerElement.Elem()
			} else {
				break
			}
		}
		// If the item is a slice, we grow it accordingly based on the size tags.
		result := growSliceFromSizeTags(val, sizes)
		reflect.Copy(result, val)
		val.Set(result)
	}
	i := uint64(1)
	for i < endOffset {
		if val.Type() == typ {
			growConcreteSliceType(val, val.Type(), int(i)+1)
		}
		index, err = factory.Unmarshal(val.Index(int(i)), typ.Elem(), input, index)
		if err != nil {
			return 0, err
		}
		i++
	}
	return index, nil
}
