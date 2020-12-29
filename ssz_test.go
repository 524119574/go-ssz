package ssz

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-ssz/types"
)

type beaconState struct {
	BlockRoots [][]byte `ssz-size:"65536,32"`
}

type fork struct {
	PreviousVersion [4]byte
	CurrentVersion  [4]byte
	Epoch           uint64
}

type truncateSignatureCase struct {
	Slot              uint64
	PreviousBlockRoot []byte
	Signature         []byte
}

type simpleNonProtoMessage struct {
	Foo []byte
	Bar uint64
}

func TestNilElementMarshal(t *testing.T) {
	type ex struct{}
	var item *ex
	buf, err := Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, []byte{}) {
		t.Errorf("Wanted empty byte slice, got %v", buf)
	}
}

func TestNilElementDetermineSize(t *testing.T) {
	type ex struct{}
	var item *ex
	size := types.DetermineSize(reflect.ValueOf(item))
	if size != 0 {
		t.Errorf("Wanted size 0, received %d", size)
	}
}

func TestMarshalNilArray(t *testing.T) {
	type ex struct {
		Slot         uint64
		Graffiti     []byte
		DepositIndex uint64
	}
	b1 := &ex{
		Slot:         5,
		Graffiti:     nil,
		DepositIndex: 64,
	}
	b2 := &ex{
		Slot:         5,
		Graffiti:     make([]byte, 0),
		DepositIndex: 64,
	}
	enc1, err := Marshal(b1)
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := Marshal(b2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(enc1, enc2) {
		t.Errorf("First item %v != second item %v", enc1, enc2)
	}
}

func TestPartialDataMarshalUnmarshal(t *testing.T) {
	type block struct {
		Slot      uint64
		Transfers []*simpleProtoMessage
	}
	b := &block{
		Slot: 5,
	}
	enc, err := Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	dec := &block{}
	if err := Unmarshal(enc, dec); err != nil {
		t.Fatal(err)
	}
}

func TestMarshal(t *testing.T) {
	tests := []struct {
		name   string
		input  interface{}
		output []byte
		err    error
	}{
		{
			name: "Nil",
			err:  errors.New("untyped-value nil cannot be marshaled"),
		},
		{
			name:  "Unsupported",
			input: complex(1, 1),
			err:   errors.New("unsupported kind: complex128"),
		},
		{
			name:  "UnsupportedPointer",
			input: &[]complex128{complex(1, 1), complex(1, 1)},
			err:   errors.New("failed to marshal for type: []complex128: unsupported kind: complex128"),
		},
		{
			name:  "UnsupportedStructElement",
			input: struct{ Foo complex128 }{complex(1, 1)},
			err:   errors.New("failed to marshal for type: struct { Foo complex128 }: unsupported kind: complex128"),
		},
		{
			name:   "Simple",
			input:  struct{ Foo uint32 }{12345},
			output: []byte{0x39, 0x30, 0x00, 0x00},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := Marshal(test.input)
			if test.err == nil {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if bytes.Compare(test.output, output) != 0 {
					t.Errorf("incorrect output: expected %v; received %v", test.output, output)
				}
			} else {
				if err == nil {
					t.Fatalf("missing expected error %v", test.err)
				}
				if test.err.Error() != err.Error() {
					t.Errorf("incorrect error: expected %v; received %v", test.err, err)
				}
			}
		})
	}
}

func TestUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		output interface{}
		err    error
	}{
		{
			name: "Nil",
			err:  errors.New("cannot unmarshal into untyped, nil value"),
		},
		{
			name:   "NotPointer",
			input:  []byte{0x00, 0x00, 0x00, 0x00},
			output: "",
			err:    errors.New("can only unmarshal into a pointer target"),
		},
		{
			name:   "OutputNotSupported",
			input:  []byte{0x00, 0x00, 0x00, 0x00},
			output: &struct{ Foo complex128 }{complex(1, 1)},
			err:    errors.New("could not unmarshal input into type: struct { Foo complex128 }: unsupported kind: complex128"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Unmarshal(test.input, test.output)
			if test.err == nil {
				if err != nil {
					t.Errorf("unexpected error %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("missing expected error %v", test.err)
				}
				if test.err.Error() != err.Error() {
					t.Errorf("unexpected error value %v (expected %v)", err, test.err)
				}
			}
		})
	}
}

func TestNilInstantiationMarshalEquality(t *testing.T) {
	type exampleBody struct {
		Epoch uint64
	}
	type example struct {
		Slot uint64
		Root [32]byte
		Body *exampleBody
	}
	root := [32]byte{1, 2, 3, 4}
	item := &example{
		Slot: 5,
		Root: root,
		Body: nil,
	}
	item2 := &example{
		Slot: 5,
		Root: root,
		Body: &exampleBody{},
	}
	enc, err := Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := Marshal(item2)
	if err != nil {
		t.Fatal(err)
	}
	dec := &example{}
	if err := Unmarshal(enc, dec); err != nil {
		t.Fatal(err)
	}
	dec2 := &example{}
	if err := Unmarshal(enc2, dec2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(enc, enc2) {
		t.Errorf("Unequal marshalings %v != %v", enc, enc2)
	}
}

func TestEmptyDataUnmarshal(t *testing.T) {
	msg := &simpleProtoMessage{}
	if err := Unmarshal([]byte{}, msg); err == nil {
		t.Error("Expected unmarshal to fail when attempting to unmarshal from an empty byte slice")
	}
}

func toBytes32(x []byte) [32]byte {
	var y [32]byte
	copy(y[:], x)
	return y
}

func TestBoolArray_MissingByte(t *testing.T) {
	objBytes := hexDecodeOrDie(t, "010101010101010101010101010101")
	var result [16]bool
	if err := Unmarshal(objBytes, &result); err == nil {
		t.Error("Expected message with missing byte to fail marshalling")
	}
}

func TestBoolArray_ExtraByte(t *testing.T) {
	objBytes := hexDecodeOrDie(t, "01010101010101010101010101010101ff")
	var result [16]bool
	if err := Unmarshal(objBytes, &result); err == nil {
		t.Error("Expected message with extra byte to fail marshalling")
	}
}

func TestBoolArray_Correct(t *testing.T) {
	objBytes := hexDecodeOrDie(t, "01010101010101010101010101010101")
	var result [16]bool
	if err := Unmarshal(objBytes, &result); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func hexDecodeOrDie(t *testing.T, s string) []byte {
	res, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
