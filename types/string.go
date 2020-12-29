package types

import (
	"reflect"
)

type stringSSZ struct{}

func newStringSSZ() *stringSSZ {
	return &stringSSZ{}
}

func (b *stringSSZ) Marshal(val reflect.Value, typ reflect.Type, buf []byte, startOffset uint64) (uint64, error) {
	for i := 0; i < val.Len(); i++ {
		buf[int(startOffset)+i] = uint8(val.Index(i).Uint())
	}
	return startOffset + uint64(val.Len()), nil
}

func (b *stringSSZ) Unmarshal(val reflect.Value, typ reflect.Type, input []byte, startOffset uint64) (uint64, error) {
	offset := startOffset + uint64(len(input))
	val.SetString(string(input[startOffset:offset]))
	return offset, nil
}
