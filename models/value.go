package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ValidateValue reports whether value parses as a value of type t, using the
// same format the generated clients read: scalars in their Go text form, and
// arrays and sets as a JSON array of the element type.
func ValidateValue(t Type, value string) error {
	var err error
	switch t {
	case TypeString:
		return nil
	case TypeInt:
		_, err = strconv.ParseInt(value, 10, 64)
	case TypeBool:
		_, err = strconv.ParseBool(value)
	case TypeFloat:
		_, err = strconv.ParseFloat(value, 64)
	case TypeStringArray, TypeStringSet:
		err = parseJSONArray[string](value)
	case TypeIntArray, TypeIntSet:
		err = parseJSONArray[int64](value)
	case TypeBoolArray, TypeBoolSet:
		err = parseJSONArray[bool](value)
	case TypeFloatArray, TypeFloatSet:
		err = parseJSONArray[float64](value)
	default:
		return fmt.Errorf("unknown Item type %q", t)
	}
	if err != nil {
		return fmt.Errorf("%q isn't a valid %s: %w", value, t, err)
	}

	return nil
}

func parseJSONArray[T any](value string) error {
	var elems []T
	if err := json.Unmarshal([]byte(value), &elems); err != nil {
		return err
	}
	if elems == nil {
		return errors.New("null is not an array")
	}

	return nil
}
