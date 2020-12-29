package ssz

import (
	"fmt"
	"reflect"

	fssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"github.com/524119574/go-ssz/types"
)

// Marshal a value and output the result into a byte slice.
// Given a struct with the following fields, one can marshal it as follows:
//  type exampleStruct struct {
//      Field1 uint8
//      Field2 []byte
//  }
//
//  ex := exampleStruct{
//      Field1: 10,
//      Field2: []byte{1, 2, 3, 4},
//  }
//  encoded, err := Marshal(ex)
//  if err != nil {
//      return fmt.Errorf("failed to marshal: %v", err)
//  }
//
// One can also specify the specific size of a struct's field by using
// ssz-specific field tags as follows:
//
//  type exampleStruct struct {
//      Field1 uint8
//      Field2 []byte `ssz:"size=32"`
//  }
//
// This will treat `Field2` as as [32]byte array when marshaling. For unbounded
// fields or multidimensional slices, ssz size tags can also be used as follows:
//
//  type exampleStruct struct {
//      Field1 uint8
//      Field2 [][]byte `ssz:"size=?,32"`
//  }
//
// This will treat `Field2` as type [][32]byte when marshaling a
// struct of that type.
func Marshal(val interface{}) ([]byte, error) {
	if val == nil {
		return nil, errors.New("untyped-value nil cannot be marshaled")
	}

	if v, ok := val.(fssz.Marshaler); ok {
		return v.MarshalSSZ()
	}

	rval := reflect.ValueOf(val)

	// We pre-allocate a buffer-size depending on the value's calculated total byte size.
	buf := make([]byte, types.DetermineSize(rval))
	factory, err := types.SSZFactory(rval, rval.Type())
	if err != nil {
		return nil, err
	}
	if rval.Type().Kind() == reflect.Ptr {
		if rval.IsNil() {
			return buf, nil
		}
		if _, err := factory.Marshal(rval.Elem(), rval.Type().Elem(), buf, 0 /* start offset */); err != nil {
			return nil, errors.Wrapf(err, "failed to marshal for type: %v", rval.Type().Elem())
		}
		return buf, nil
	}
	if _, err := factory.Marshal(rval, rval.Type(), buf, 0 /* start offset */); err != nil {
		return nil, errors.Wrapf(err, "failed to marshal for type: %v", rval.Type())
	}
	return buf, nil
}

// Unmarshal SSZ encoded data and output it into the object pointed by pointer val.
// Given a struct with the following fields, and some encoded bytes of type []byte,
// one can then unmarshal the bytes into a pointer of the struct as follows:
//  type exampleStruct1 struct {
//      Field1 uint8
//      Field2 []byte
//  }
//
//  var targetStruct exampleStruct1
//  if err := Unmarshal(encodedBytes, &targetStruct); err != nil {
//      return fmt.Errorf("failed to unmarshal: %v", err)
//  }
func Unmarshal(input []byte, val interface{}) error {
	if val == nil {
		return errors.New("cannot unmarshal into untyped, nil value")
	}
	if v, ok := val.(fssz.Unmarshaler); ok {
		return v.UnmarshalSSZ(input)
	}
	if len(input) == 0 {
		return errors.New("no data to unmarshal from, input is an empty byte slice []byte{}")
	}
	rval := reflect.ValueOf(val)
	rtyp := rval.Type()
	// val must be a pointer, otherwise we refuse to unmarshal
	if rtyp.Kind() != reflect.Ptr {
		return errors.New("can only unmarshal into a pointer target")
	}
	if rval.IsNil() {
		return errors.New("cannot output to pointer of nil value")
	}
	factory, err := types.SSZFactory(rval.Elem(), rtyp.Elem())
	if err != nil {
		return err
	}
	if _, err := factory.Unmarshal(rval.Elem(), rval.Elem().Type(), input, 0); err != nil {
		return errors.Wrapf(err, "could not unmarshal input into type: %v", rval.Elem().Type())
	}

	fixedSize := types.DetermineSize(rval)
	totalLength := uint64(len(input))
	if totalLength != fixedSize {
		return fmt.Errorf(
			"unexpected amount of data, expected: %d, received: %d",
			fixedSize,
			totalLength,
		)
	}
	return nil
}
