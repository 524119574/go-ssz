package types

import (
	"reflect"
	"sync"

	"github.com/dgraph-io/ristretto"
)

// BasicArraySizeCache for HashTreeRoot.
const BasicArraySizeCache = 100000

var fastSumHashKey = toBytes32([]byte("hash_fast_sum64_key"))

type basicArraySSZ struct {
	hashCache *ristretto.Cache
	lock      sync.Mutex
}

func newBasicArraySSZ() *basicArraySSZ {
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: BasicArraySizeCache, // number of keys to track frequency of (1M).
		MaxCost:     1 << 22,             // maximum cost of cache (3MB).
		// 100,000 roots will take up approximately 3 MB in memory.
		BufferItems: 64, // number of keys per Get buffer.
	})
	return &basicArraySSZ{
		hashCache: cache,
	}
}

func (b *basicArraySSZ) Marshal(val reflect.Value, typ reflect.Type, buf []byte, startOffset uint64) (uint64, error) {
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

func (b *basicArraySSZ) Unmarshal(val reflect.Value, typ reflect.Type, input []byte, startOffset uint64) (uint64, error) {
	i := 0
	index := startOffset
	size := val.Len()
	var err error
	var factory SSZAble
	for i < size {
		if val.Index(i).Kind() == reflect.Ptr {
			instantiateConcreteTypeForElement(val.Index(i), typ.Elem().Elem())
			factory, err = SSZFactory(val.Index(i), typ.Elem().Elem())
			if err != nil {
				return 0, err
			}
		} else {
			factory, err = SSZFactory(val.Index(i), typ.Elem())
			if err != nil {
				return 0, err
			}
		}
		index, err = factory.Unmarshal(val.Index(i), typ.Elem(), input, index)
		if err != nil {
			return 0, err
		}
		i++
	}
	return index, nil
}
