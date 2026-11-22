// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package bytesize

import (
	"fmt"
	"math"
	"regexp"
	"strconv"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

const (
	_ = 1 << (10 * iota)
	KB
	MB
	GB
	TB
	PB
)

type Int64 int64

func (b Int64) Int64() int64 {
	return int64(b)
}

func (b Int64) AsInt() int {
	return int(b)
}

func (b Int64) HumanString() string {
	switch abs := math.Abs(float64(b)); {
	case abs > PB:
		return fmt.Sprintf("%.2fpb", float64(b)/PB)
	case abs > TB:
		return fmt.Sprintf("%.2ftb", float64(b)/TB)
	case abs > GB:
		return fmt.Sprintf("%.2fgb", float64(b)/GB)
	case abs > MB:
		return fmt.Sprintf("%.2fmb", float64(b)/MB)
	case abs > KB:
		return fmt.Sprintf("%.2fkb", float64(b)/KB)
	default:
		return fmt.Sprintf("%d", b.Int64())
	}
}

func (b Int64) MarshalText() ([]byte, error) {
	if b == 0 {
		return []byte("0"), nil
	}
	var abs = int64(b)
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs%PB == 0:
		val := b.Int64() / PB
		return []byte(fmt.Sprintf("%dpb", val)), nil
	case abs%TB == 0:
		val := b.Int64() / TB
		return []byte(fmt.Sprintf("%dtb", val)), nil
	case abs%GB == 0:
		val := b.Int64() / GB
		return []byte(fmt.Sprintf("%dgb", val)), nil
	case abs%MB == 0:
		val := b.Int64() / MB
		return []byte(fmt.Sprintf("%dmb", val)), nil
	case abs%KB == 0:
		val := b.Int64() / KB
		return []byte(fmt.Sprintf("%dkb", val)), nil
	default:
		return []byte(fmt.Sprintf("%d", b.Int64())), nil
	}
}

func (p *Int64) UnmarshalText(text []byte) error {
	n, err := Parse(string(text))
	if err != nil {
		return err
	}
	*p = Int64(n)
	return nil
}

// sizeRegexp accepts an optional minus sign, a decimal number, an optional
// unit ([k|m|g|t|p][b] or a bare [b|k|m|g|t|p]) and surrounding whitespace.
// It is the only pattern used for validation, so parsing does a single regexp
// pass and the number itself is classified by isInteger below.
var sizeRegexp = regexp.MustCompile(`^\s*(\-?[\d\.]+)\s*([kmgtp]?b|[bkmgtp]|)\s*$`)

var (
	ErrBadByteSize     = errors.New("invalid bytesize")
	ErrBadByteSizeUnit = errors.New("invalid bytesize unit")
)

// isInteger reports whether s is an optionally negative run of ASCII digits,
// i.e. what the former `^\-?\d+$` pattern matched.
func isInteger(s string) bool {
	if s != "" && s[0] == '-' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func Parse(s string) (int64, error) {
	subs := sizeRegexp.FindStringSubmatch(s)
	if len(subs) != 3 {
		return 0, errors.Trace(ErrBadByteSize)
	}

	text := subs[1]
	unit := subs[2]

	size := int64(1)
	switch unit {
	case "b", "":
	case "k", "kb":
		size = KB
	case "m", "mb":
		size = MB
	case "g", "gb":
		size = GB
	case "t", "tb":
		size = TB
	case "p", "pb":
		size = PB
	default:
		return 0, errors.Trace(ErrBadByteSizeUnit)
	}

	if isInteger(text) {
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, errors.Trace(ErrBadByteSize)
		}
		size *= n
	} else {
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, errors.Trace(ErrBadByteSize)
		}
		size = int64(float64(size) * n)
	}
	return size, nil
}

func MustParse(s string) int64 {
	v, err := Parse(s)
	if err != nil {
		log.PanicError(err, "parse bytesize failed")
	}
	return v
}
