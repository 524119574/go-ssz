package ssz

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
)

// decodeError is what gets reported to the decoder user in error case.
type decodeError struct {
	msg string
	typ reflect.Type
}

func newDecodeError(msg string, typ reflect.Type) *decodeError {
	return &decodeError{msg, typ}
}

func (err *decodeError) Error() string {
	return fmt.Sprintf("decode error: %s for output type %v", err.msg, err.typ)
}

// Decode SSZ encoded data and output it into the object pointed by pointer val.
func Decode(input []byte, val interface{}) error {
	if val == nil {
		return newDecodeError("cannot decode into nil", nil)
	}
	rval := reflect.ValueOf(val)
	rtyp := rval.Type()
	// val must be a pointer, otherwise we refuse to decode
	if rtyp.Kind() != reflect.Ptr {
		return newDecodeError("can only decode into pointer target", rtyp)
	}
	if rval.IsNil() {
		return newDecodeError("cannot output to pointer of nil", rtyp)
	}
	sszUtils, err := cachedSSZUtils(rval.Elem().Type())
	if err != nil {
		return newDecodeError(fmt.Sprint(err), rval.Elem().Type())
	}
	if _, err = sszUtils.decoder(input, rval.Elem(), 0); err != nil {
		return newDecodeError(fmt.Sprint(err), rval.Elem().Type())
	}
	return nil
}

func makeDecoder(typ reflect.Type) (dec decoder, err error) {
	kind := typ.Kind()
	switch {
	case kind == reflect.Bool:
		return decodeBool, nil
	case kind == reflect.Uint8:
		return decodeUint8, nil
	case kind == reflect.Uint16:
		return decodeUint16, nil
	case kind == reflect.Uint32:
		return decodeUint32, nil
	case kind == reflect.Int32:
		return decodeUint32, nil
	case kind == reflect.Uint64:
		return decodeUint64, nil
	case kind == reflect.Slice && typ.Elem().Kind() == reflect.Uint8:
		return makeByteSliceDecoder()
	case kind == reflect.Slice && isBasicType(typ.Elem().Kind()):
		return makeBasicSliceDecoder(typ)
	case kind == reflect.Slice && isBasicTypeArray(typ.Elem(), typ.Elem().Kind()):
		return makeBasicSliceDecoder(typ)
	case kind == reflect.Slice && isBasicTypeArray(typ.Elem(), typ.Elem().Kind()):
		return makeBasicSliceDecoder(typ)
	case kind == reflect.Slice:
		return makeCompositeSliceDecoder(typ)
	case kind == reflect.Array && isBasicType(typ.Elem().Kind()):
		return makeBasicArrayDecoder(typ)
	case kind == reflect.Array && isBasicTypeArray(typ.Elem(), typ.Elem().Kind()):
		return makeBasicArrayDecoder(typ)
	case kind == reflect.Array:
		return makeCompositeArrayDecoder(typ)
	case kind == reflect.Struct:
		return makeStructDecoder(typ)
	case kind == reflect.Ptr:
		return makePtrDecoder(typ)
	default:
		return nil, fmt.Errorf("type %v is not deserializable", typ)
	}
}

func decodeBool(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
	v := uint8(input[startOffset])
	if v == 0 {
		val.SetBool(false)
	} else if v == 1 {
		val.SetBool(true)
	} else {
		return 0, fmt.Errorf("expect 0 or 1 for decoding bool but got %d", v)
	}
	return startOffset + 1, nil
}

func decodeUint8(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
	val.SetUint(uint64(input[startOffset]))
	return startOffset + 1, nil
}

func decodeUint16(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
	offset := startOffset + 2
	buf := make([]byte, 2)
	copy(buf, input[startOffset:offset])
	val.SetUint(uint64(binary.LittleEndian.Uint16(buf)))
	return offset, nil
}

func decodeUint32(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
	offset := startOffset + 4
	buf := make([]byte, 4)
	copy(buf, input[startOffset:offset])
	val.SetUint(uint64(binary.LittleEndian.Uint32(buf)))
	return offset, nil
}

func decodeUint64(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
	offset := startOffset + 8
	buf := make([]byte, 8)
	copy(buf, input[startOffset:offset])
	val.SetUint(binary.LittleEndian.Uint64(buf))
	return offset, nil
}

func makeByteSliceDecoder() (decoder, error) {
	decoder := func(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
		offset := startOffset + uint64(len(input))
		val.SetBytes(input[startOffset:offset])
		return offset, nil
	}
	return decoder, nil
}

func makeBasicSliceDecoder(typ reflect.Type) (decoder, error) {
	elemSSZUtils, err := cachedSSZUtilsNoAcquireLock(typ.Elem())
	if err != nil {
		return nil, err
	}
	decoder := func(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
		newVal := reflect.MakeSlice(val.Type(), 1, 1)
		reflect.Copy(newVal, val)
		val.Set(newVal)

		index := startOffset
		index, err = elemSSZUtils.decoder(input, val.Index(0), index)
		if err != nil {
			return 0, fmt.Errorf("failed to decode element of slice: %v", err)
		}
		elementSize := index - startOffset
		endOffset := uint64(len(input)) / elementSize

		newVal = reflect.MakeSlice(val.Type(), int(endOffset), int(endOffset))
		reflect.Copy(newVal, val)
		val.Set(newVal)
		i := uint64(1)
		for i < endOffset {
			index, err = elemSSZUtils.decoder(input, val.Index(int(i)), index)
			if err != nil {
				return 0, fmt.Errorf("failed to decode element of slice: %v", err)
			}
			i++
		}
		return index, nil
	}
	return decoder, nil
}

func makeCompositeSliceDecoder(typ reflect.Type) (decoder, error) {
	elemType := typ.Elem()
	elemSSZUtils, err := cachedSSZUtilsNoAcquireLock(elemType)
	if err != nil {
		return nil, err
	}
	decoder := func(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
		// TODO: Limitation, creating a list of type pointers creates a list of nil values.
		newVal := reflect.MakeSlice(typ, 1, 1)
		reflect.Copy(newVal, val)
		val.Set(newVal)
		endOffset := uint64(len(input))

		currentIndex := startOffset
		nextIndex := currentIndex
		offsetVal := input[startOffset : startOffset+uint64(BytesPerLengthOffset)]
		firstOffset := startOffset + uint64(binary.LittleEndian.Uint32(offsetVal))
		currentOffset := firstOffset
		nextOffset := currentOffset
		i := 0
		for currentIndex < firstOffset {
			if currentOffset > endOffset {
				return 0, errors.New("offset out of bounds")
			}
			nextIndex = currentIndex + uint64(BytesPerLengthOffset)
			if nextIndex == firstOffset {
				nextOffset = endOffset
			} else {
				nextOffsetVal := input[nextIndex : nextIndex+uint64(BytesPerLengthOffset)]
				nextOffset = startOffset + uint64(binary.LittleEndian.Uint32(nextOffsetVal))
			}
			if currentOffset > nextOffset {
				return 0, errors.New("offsets must be increasing")
			}
			// We grow the slice's size to accommodate a new element being decoded.
			newVal := reflect.MakeSlice(typ, i+1, i+1)
			reflect.Copy(newVal, val)
			val.Set(newVal)
			if _, err := elemSSZUtils.decoder(input[currentOffset:nextOffset], val.Index(i), 0); err != nil {
				return 0, fmt.Errorf("failed to decode element of slice: %v", err)
			}
			i++
			currentIndex = nextIndex
			currentOffset = nextOffset
		}
		return currentIndex, nil
	}
	return decoder, nil
}

func makeBasicArrayDecoder(typ reflect.Type) (decoder, error) {
	elemType := typ.Elem()
	elemSSZUtils, err := cachedSSZUtilsNoAcquireLock(elemType)
	if err != nil {
		return nil, err
	}
	decoder := func(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
		i := 0
		index := startOffset
		size := val.Len()
		for i < size {
			index, err = elemSSZUtils.decoder(input, val.Index(i), index)
			if err != nil {
				return 0, fmt.Errorf("failed to decode element of array: %v", err)
			}
			i++
		}
		return index, nil
	}
	return decoder, nil
}

func makeCompositeArrayDecoder(typ reflect.Type) (decoder, error) {
	elemType := typ.Elem()
	elemSSZUtils, err := cachedSSZUtilsNoAcquireLock(elemType)
	if err != nil {
		return nil, err
	}
	decoder := func(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
		currentIndex := startOffset
		nextIndex := currentIndex
		offsetVal := input[startOffset : startOffset+uint64(BytesPerLengthOffset)]
		firstOffset := startOffset + uint64(binary.LittleEndian.Uint32(offsetVal))
		currentOffset := firstOffset
		nextOffset := currentOffset
		endOffset := uint64(len(input))

		i := 0
		for currentIndex < firstOffset {
			if currentOffset > endOffset {
				return 0, errors.New("offset out of bounds")
			}
			nextIndex = currentIndex + uint64(BytesPerLengthOffset)
			if nextIndex == firstOffset {
				nextOffset = endOffset
			} else {
				nextOffsetVal := input[nextIndex : nextIndex+uint64(BytesPerLengthOffset)]
				nextOffset = startOffset + uint64(binary.LittleEndian.Uint32(nextOffsetVal))
			}
			if currentOffset > nextOffset {
				return 0, errors.New("offsets must be increasing")
			}
			if _, err := elemSSZUtils.decoder(input[currentOffset:nextOffset], val.Index(i), 0); err != nil {
				return 0, fmt.Errorf("failed to decode element of slice: %v", err)
			}
			i++
			currentIndex = nextIndex
			currentOffset = nextOffset
		}
		return currentIndex, nil
	}
	return decoder, nil
}

func makeStructDecoder(typ reflect.Type) (decoder, error) {
	fields, err := structFields(typ)
	if err != nil {
		return nil, err
	}
	decoder := func(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
		endOffset := uint64(len(input))
		currentIndex := startOffset
		nextIndex := currentIndex
		fixedSizes := make([]uint64, len(fields))

		for i := 0; i < len(fixedSizes); i++ {
			fixedSz := determineFixedSize(val.Field(i), val.Field(i).Type())
			if !isVariableSizeType(val.Field(i), val.Field(i).Type()) && (fixedSz > 0) {
				fixedSizes[i] = fixedSz
			} else {
				fixedSizes[i] = 0
			}
		}

		offsets := make([]uint64, 0)
		fixedEnd := uint64(0)
		for i, item := range fixedSizes {
			if item > 0 {
				fixedEnd += uint64(i) + item
			} else {
				offsetVal := input[i : i+BytesPerLengthOffset]
				offsets = append(offsets, startOffset+binary.LittleEndian.Uint64(offsetVal))
				fixedEnd += uint64(i + BytesPerLengthOffset)
			}
		}
		offsets = append(offsets, endOffset)

		offsetIndex := uint64(0)
		for i := 0; i < len(fields); i++ {
			f := fields[i]
			fieldSize := fixedSizes[i]
			if fieldSize > 0 {
				nextIndex = currentIndex + fieldSize
				if _, err := f.sszUtils.decoder(input[currentIndex:nextIndex], val.Field(i), 0); err != nil {
					return 0, err
				}
				currentIndex = nextIndex

			} else {
				firstOff := offsets[offsetIndex]
				nextOff := offsets[offsetIndex+1]
				if _, err := f.sszUtils.decoder(input[firstOff:nextOff], val.Field(i), 0); err != nil {
					return 0, err
				}
				offsetIndex++
				currentIndex += uint64(BytesPerLengthOffset)
			}
		}
		return 0, nil
	}
	return decoder, nil
}

func makePtrDecoder(typ reflect.Type) (decoder, error) {
	elemType := typ.Elem()
	elemSSZUtils, err := cachedSSZUtilsNoAcquireLock(elemType)
	if err != nil {
		return nil, err
	}
	decoder := func(input []byte, val reflect.Value, startOffset uint64) (uint64, error) {
		elemDecodeSize, err := elemSSZUtils.decoder(input, val.Elem(), startOffset)
		if err != nil {
			return 0, fmt.Errorf("failed to decode to object pointed by pointer: %v", err)
		}
		return elemDecodeSize, nil
	}
	return decoder, nil
}
