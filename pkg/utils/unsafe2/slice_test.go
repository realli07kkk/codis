// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package unsafe2

import (
	"testing"

	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestMakeGoSlice(t *testing.T) {
	n := MinOffheapSlice - 1
	s := MakeSlice(n)
	assert.Must(t, s != nil)
	_, ok := s.(*goSlice)
	assert.Must(t, ok)

	c1 := s.Slice2(0, 1)
	c2 := s.Slice2(0, 2)
	assert.Must(t, c1.Parent() == nil)
	assert.Must(t, c2.Parent() == nil)
}

func TestMakeCGoSlice(t *testing.T) {
	n := MinOffheapSlice * 2
	SetMaxOffheapBytes(int64(n) * 2)

	s1 := MakeSlice(n)
	_, ok1 := s1.(*cgoSlice)
	assert.Must(t, ok1 && len(s1.Buffer()) == n)
	defer FreeSlice(s1)

	s2 := MakeSlice(n)
	_, ok2 := s2.(*cgoSlice)
	assert.Must(t, ok2 && len(s2.Buffer()) == n)
	defer FreeSlice(s2)

	assert.Must(t, OffheapBytes() == int64(n)*2)

	s3 := MakeSlice(n)
	_, ok3 := s3.(*goSlice)
	assert.Must(t, ok3 && len(s3.Buffer()) == n)
	defer FreeSlice(s3)

	assert.Must(t, OffheapBytes() == int64(n)*2)

	FreeSlice(s2)
	assert.Must(t, OffheapBytes() == int64(n))

	s4 := MakeSlice(n)
	_, ok4 := s4.(*cgoSlice)
	assert.Must(t, ok4 && len(s4.Buffer()) == n)
	defer FreeSlice(s4)

	assert.Must(t, OffheapBytes() == int64(n)*2)

	s5 := MakeOffheapSlice(n)
	assert.Must(t, s5 != nil && len(s5.Buffer()) == n)
	defer FreeSlice(s5)

	assert.Must(t, OffheapBytes() == int64(n)*3)

	c1 := s5.Slice2(0, 2)
	c2 := c1.Slice3(0, 1, 2)
	assert.Must(t, c1.Parent() == s5)
	assert.Must(t, c2.Parent() == s5)

	c1.Buffer()[0] = 'a'
	assert.Must(t, c2.Buffer()[0] == 'a')

	c1.Buffer()[0] = 'b'
	assert.Must(t, c2.Buffer()[0] == 'b')
}
