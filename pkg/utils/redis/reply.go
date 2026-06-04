// Copyright 2026 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redis

import (
	"strconv"

	"github.com/CodisLabs/codis/pkg/utils/errors"
)

func replyError(reply interface{}) error {
	if err, ok := reply.(error); ok {
		return errors.Trace(err)
	}
	return nil
}

func replyArgError(errs []error) error {
	if len(errs) == 0 || errs[0] == nil {
		return nil
	}
	return errors.Trace(errs[0])
}

func replyString(reply interface{}, errs ...error) (string, error) {
	if err := replyArgError(errs); err != nil {
		return "", err
	}
	if err := replyError(reply); err != nil {
		return "", err
	}
	switch v := reply.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return "", errors.Errorf("invalid response = %v", reply)
	}
}

func replyInt(reply interface{}, errs ...error) (int, error) {
	if err := replyArgError(errs); err != nil {
		return 0, err
	}
	if err := replyError(reply); err != nil {
		return 0, err
	}
	switch v := reply.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		return int(v), nil
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, errors.Trace(err)
		}
		return n, nil
	case []byte:
		n, err := strconv.Atoi(string(v))
		if err != nil {
			return 0, errors.Trace(err)
		}
		return n, nil
	default:
		return 0, errors.Errorf("invalid response = %v", reply)
	}
}

func replyValues(reply interface{}, errs ...error) ([]interface{}, error) {
	if err := replyArgError(errs); err != nil {
		return nil, err
	}
	if err := replyError(reply); err != nil {
		return nil, err
	}
	switch v := reply.(type) {
	case []interface{}:
		return v, nil
	case []string:
		values := make([]interface{}, len(v))
		for i := range v {
			values[i] = v[i]
		}
		return values, nil
	case []int:
		values := make([]interface{}, len(v))
		for i := range v {
			values[i] = v[i]
		}
		return values, nil
	default:
		return nil, errors.Errorf("invalid response = %v", reply)
	}
}

func replyStrings(reply interface{}, errs ...error) ([]string, error) {
	values, err := replyValues(reply, errs...)
	if err != nil {
		return nil, err
	}
	strings := make([]string, len(values))
	for i, value := range values {
		s, err := replyString(value)
		if err != nil {
			return nil, err
		}
		strings[i] = s
	}
	return strings, nil
}

func replyInts(reply interface{}, errs ...error) ([]int, error) {
	values, err := replyValues(reply, errs...)
	if err != nil {
		return nil, err
	}
	ints := make([]int, len(values))
	for i, value := range values {
		n, err := replyInt(value)
		if err != nil {
			return nil, err
		}
		ints[i] = n
	}
	return ints, nil
}

func replyStringMap(reply interface{}, errs ...error) (map[string]string, error) {
	values, err := replyValues(reply, errs...)
	if err != nil {
		return nil, err
	}
	if len(values)%2 != 0 {
		return nil, errors.Errorf("invalid response = %v", reply)
	}
	m := make(map[string]string, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, err := replyString(values[i])
		if err != nil {
			return nil, err
		}
		value, err := replyString(values[i+1])
		if err != nil {
			return nil, err
		}
		m[key] = value
	}
	return m, nil
}
