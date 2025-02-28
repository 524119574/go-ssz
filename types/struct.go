package types

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"log"
	"github.com/pkg/errors"
)

// UnboundedSSZFieldSizeMarker is the character used to specify a ssz field should have
// unbounded size, which is useful when describing slices of arrays such as [][32]byte.
// The ssz struct tag for such field type would be `ssz:"size=?,32"`. A question mark
// is chosen as the default value given its simplicity to represent unbounded size.
var UnboundedSSZFieldSizeMarker = "?"

type structSSZ struct{}

func newStructSSZ() *structSSZ {
	return &structSSZ{}
}

func (b *structSSZ) Marshal(val reflect.Value, typ reflect.Type, buf []byte, startOffset uint64) (uint64, error) {
	log.Printf("weiwu")
	if typ.Kind() == reflect.Ptr {
		if val.IsNil() {
			newVal := reflect.New(typ.Elem()).Elem()
			return b.Marshal(newVal, newVal.Type(), buf, startOffset)
		}
		return b.Marshal(val.Elem(), typ.Elem(), buf, startOffset)
	}
	fixedIndex := startOffset
	fixedLength := uint64(0)
	// For every field, we add up the total length of the items depending if they
	// are variable or fixed-size fields.
	for i := 0; i < typ.NumField(); i++ {
		fType, err := determineFieldType(typ.Field(i))
		if err != nil {
			return 0, err
		}
		if isVariableSizeType(fType) {
			fixedLength += BytesPerLengthOffset
		} else {
			if val.Type().Kind() == reflect.Ptr && val.IsNil() {
				elem := reflect.New(val.Type().Elem()).Elem()
				fixedLength += determineFixedSize(elem, fType)
			} else {
				fixedLength += determineFixedSize(val.Field(i), fType)
			}
		}
		log.Printf("fixed length: %d", fixedLength)
	}
	currentOffsetIndex := startOffset + fixedLength
	for i := 0; i < typ.NumField(); i++ {
		fType, err := determineFieldType(typ.Field(i))
		if err != nil {
			return 0, err
		}
		factory, err := SSZFactory(val.Field(i), fType)
		if err != nil {
			return 0, err
		}
		if !isVariableSizeType(fType) {
			fixedIndex, err = factory.Marshal(val.Field(i), fType, buf, fixedIndex)
			if err != nil {
				return 0, err
			}
		} else {
			nextOffsetIndex, err := factory.Marshal(val.Field(i), fType, buf, currentOffsetIndex)
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
		log.Printf("current offset index: %d, fixed index: %d", currentOffsetIndex, fixedIndex)
	}
	return currentOffsetIndex, nil
}

func (b *structSSZ) Unmarshal(val reflect.Value, typ reflect.Type, input []byte, startOffset uint64) (uint64, error) {
	if typ.Kind() == reflect.Ptr {
		if val.IsNil() {
			return startOffset, nil
		}
		return b.Unmarshal(val.Elem(), typ.Elem(), input, startOffset)
	}
	endOffset := uint64(len(input))
	currentIndex := startOffset
	nextIndex := currentIndex
	numFields := 0

	for i := 0; i < typ.NumField(); i++ {
		// We skip protobuf related metadata fields.
		if strings.Contains(typ.Field(i).Name, "XXX_") {
			continue
		}
		numFields++
	}

	fixedSizes := make(map[int]uint64)
	for i := 0; i < numFields; i++ {
		fType, err := determineFieldType(typ.Field(i))
		if err != nil {
			return 0, err
		}
		if isVariableSizeType(fType) {
			continue
		}
		if val.Field(i).Kind() == reflect.Ptr {
			instantiateConcreteTypeForElement(val.Field(i), fType.Elem())
		}
		concreteVal := val.Field(i)
		sszSizeTags, hasTags, err := parseSSZFieldTags(typ.Field(i))
		if err != nil {
			return 0, err
		}
		if hasTags {
			concreteType := inferFieldTypeFromSizeTags(typ.Field(i), sszSizeTags)
			concreteVal = reflect.New(concreteType).Elem()
			// If the item is a slice, we grow it accordingly based on the size tags.
			if val.Field(i).Kind() == reflect.Slice {
				result := growSliceFromSizeTags(val.Field(i), sszSizeTags)
				val.Field(i).Set(result)
			}
		}
		fixedSz := determineFixedSize(concreteVal, fType)
		fixedSizes[i] = fixedSz
	}

	offsets := make([]uint64, 0)
	offsetIndexCounter := startOffset
	for i := 0; i < numFields; i++ {
		if item, ok := fixedSizes[i]; ok {
			offsetIndexCounter += item
		} else {
			if offsetIndexCounter+BytesPerLengthOffset > uint64(len(input)) {
				offsetIndexCounter += BytesPerLengthOffset
				continue
			}
			offsetVal := input[offsetIndexCounter : offsetIndexCounter+BytesPerLengthOffset]
			offsets = append(offsets, startOffset+uint64(binary.LittleEndian.Uint32(offsetVal)))
			offsetIndexCounter += BytesPerLengthOffset
		}
	}
	offsets = append(offsets, endOffset)
	offsetIndex := uint64(0)
	for i := 0; i < numFields; i++ {
		fType, err := determineFieldType(typ.Field(i))
		if err != nil {
			return 0, err
		}
		if val.Field(i).Kind() == reflect.Ptr {
			instantiateConcreteTypeForElement(val.Field(i), fType.Elem())
		}
		factory, err := SSZFactory(val.Field(i), fType)
		if err != nil {
			return 0, err
		}
		if item, ok := fixedSizes[i]; ok {
			if item == 0 {
				continue
			}
			nextIndex = currentIndex + item
			if _, err := factory.Unmarshal(val.Field(i), fType, input[currentIndex:nextIndex], 0); err != nil {
				return 0, err
			}
			currentIndex = nextIndex
		} else {
			firstOff := offsets[offsetIndex]
			if firstOff == uint64(len(input)) {
				currentIndex += BytesPerLengthOffset
				continue
			}
			nextOff := offsets[offsetIndex+1]
			if nextOff > uint64(len(input)) {
				return 0, fmt.Errorf("slice bounds out of range [%d:%d]", firstOff, nextOff)
			}
			if _, err := factory.Unmarshal(val.Field(i), fType, input[firstOff:nextOff], 0); err != nil {
				return 0, err
			}
			offsetIndex++
			currentIndex += BytesPerLengthOffset
		}
	}
	return currentIndex, nil
}

func determineFieldType(field reflect.StructField) (reflect.Type, error) {
	fieldSizeTags, exists, err := parseSSZFieldTags(field)
	if err != nil {
		return nil, errors.Wrap(err, "could not parse ssz struct field tags")
	}
	if exists {
		// If the field does indeed specify ssz struct tags, we infer the field's type.
		return inferFieldTypeFromSizeTags(field, fieldSizeTags), nil
	}
	return field.Type, nil
}

func determineFieldCapacity(field reflect.StructField) uint64 {
	tag, exists := field.Tag.Lookup("ssz-max")
	if !exists {
		return 0
	}
	val, err := strconv.ParseUint(tag, 10, 64)
	if err != nil {
		return 0
	}
	return val
}

// TODO: change this.
func parseSSZFieldTags(field reflect.StructField) ([]uint64, bool, error) {
	tag, exists := field.Tag.Lookup("ssz-size")
	if !exists {
		return nil, false, nil
	}
	items := strings.Split(tag, ",")
	sizes := make([]uint64, len(items))
	var err error
	for i := 0; i < len(items); i++ {
		// If a field is unbounded, we mark it with a size of 0.
		if items[i] == UnboundedSSZFieldSizeMarker {
			sizes[i] = 0
			continue
		}
		sizes[i], err = strconv.ParseUint(items[i], 10, 64)
		if err != nil {
			return nil, false, err
		}
	}
	return sizes, true, nil
}

func inferFieldTypeFromSizeTags(field reflect.StructField, sizes []uint64) reflect.Type {
	innerElement := field.Type.Elem()
	for i := 1; i < len(sizes); i++ {
		innerElement = innerElement.Elem()
	}
	currentType := innerElement
	for i := len(sizes) - 1; i >= 0; i-- {
		if sizes[i] == 0 {
			currentType = reflect.SliceOf(currentType)
		} else {
			currentType = reflect.ArrayOf(int(sizes[i]), currentType)
		}
	}
	return currentType
}
