package types

import (
	"encoding/binary"
	"reflect"
)

type compositeArraySSZ struct{}

func newCompositeArraySSZ() *compositeArraySSZ {
	return &compositeArraySSZ{}
}

func (b *compositeArraySSZ) Marshal(val reflect.Value, typ reflect.Type, buf []byte, startOffset uint64) (uint64, error) {
	index := startOffset
	if val.Len() == 0 {
		return index, nil
	}
	factory, err := SSZFactory(val.Index(0), typ.Elem())
	if err != nil {
		return 0, err
	}
	if !isVariableSizeType(typ.Elem()) {
		for i := 0; i < val.Len(); i++ {
			// If each element is not variable size, we simply encode sequentially and write
			// into the buffer at the last index we wrote at.
			index, err = factory.Marshal(val.Index(i), typ.Elem(), buf, index)
			if err != nil {
				return 0, err
			}
		}
		return index, nil
	}
	fixedIndex := index
	currentOffsetIndex := startOffset + uint64(val.Len())*BytesPerLengthOffset
	nextOffsetIndex := currentOffsetIndex
	// If the elements are variable size, we need to include offset indices
	// in the serialized output list.
	for i := 0; i < val.Len(); i++ {
		nextOffsetIndex, err = factory.Marshal(val.Index(i), typ.Elem(), buf, currentOffsetIndex)
		if err != nil {
			return 0, err
		}
		// Write the offset.
		offsetBuf := make([]byte, BytesPerLengthOffset)
		binary.LittleEndian.PutUint32(offsetBuf, uint32(currentOffsetIndex-startOffset))
		copy(buf[fixedIndex:fixedIndex+BytesPerLengthOffset], offsetBuf)

		// We increase the offset indices accordingly.
		currentOffsetIndex = nextOffsetIndex
		fixedIndex += BytesPerLengthOffset
	}
	index = currentOffsetIndex
	return index, nil
}

func (b *compositeArraySSZ) Unmarshal(val reflect.Value, typ reflect.Type, input []byte, startOffset uint64) (uint64, error) {
	currentIndex := startOffset
	nextIndex := currentIndex
	offsetVal := input[startOffset : startOffset+BytesPerLengthOffset]
	firstOffset := startOffset + uint64(binary.LittleEndian.Uint32(offsetVal))
	currentOffset := firstOffset
	nextOffset := currentOffset
	endOffset := uint64(len(input))
	i := 0
	if val.Kind() == reflect.Slice {
		instantiatedArray := reflect.MakeSlice(val.Type(), typ.Len(), typ.Len())
		val.Set(instantiatedArray)
	}
	factory, err := SSZFactory(val.Index(0), typ.Elem())
	if err != nil {
		return 0, err
	}
	for currentIndex < firstOffset {
		nextIndex = currentIndex + BytesPerLengthOffset
		if nextIndex == firstOffset {
			nextOffset = endOffset
		} else {
			nextOffsetVal := input[nextIndex : nextIndex+BytesPerLengthOffset]
			nextOffset = startOffset + uint64(binary.LittleEndian.Uint32(nextOffsetVal))
		}
		if val.Index(i).Kind() == reflect.Ptr {
			instantiateConcreteTypeForElement(val.Index(i), typ.Elem().Elem())
		}
		if _, err := factory.Unmarshal(val.Index(i), typ.Elem(), input[currentOffset:nextOffset], 0); err != nil {
			return 0, err
		}
		i++
		currentIndex = nextIndex
		currentOffset = nextOffset
	}
	return currentIndex, nil
}
