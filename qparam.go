// Copyright (c) 2017, A. Stoewer <adrian.stoewer@rz.ifi.lmu.de>
// All rights reserved.

package qparam

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/stoewer/go-qparam/internal"
)

var (
	defaultTag    = "param"
	defaultMapper = strings.ToLower
)

// Option is a functional option which can be applied to a reader.
type Option func(*Reader)

// Mapper is a functional option which allows to specify a custom name mapper to
// the reader.
func Mapper(mapper func(string) string) Option {
	return func(r *Reader) {
		r.mapper = mapper
	}
}

// Tag is a functional option which allows to specify a custom struct tag for the
// reader.
func Tag(tag string) Option {
	return func(r *Reader) {
		r.tag = tag
	}
}

// Strict is a functional option used to define whether the reader runs in struct
// mode or not. In strict mode all parsed values must have an equivalent target field.
// If the strict rule is violated the Reader returns an error.
func Strict(strict bool) Option {
	return func(r *Reader) {
		r.strict = strict
	}
}

// Reader defines methods which can read query parameters and assign them to matching
// fields of target structs.
type Reader struct {
	tag    string
	strict bool
	mapper func(string) string
}

// NewReader creates a new reader which can be configured with predefined functional options. The options
// can be used to configure the following reader behaviour: custom field name mapping (default: lower
// case), custom field tag (default: param) and strict mode (default: false)
func NewReader(options ...Option) *Reader {
	r := &Reader{tag: defaultTag, mapper: defaultMapper}

	for _, opt := range options {
		opt(r)
	}

	return r
}

// Read takes the provided query parameter and assigns them to the matching fields of the
// target structs.
//
// If an error occurs while parsing the values for struct fields, the returned error probably
// implements the interface MultiError. In that case specific errors for each failed field
// can be obtained from the error.
func (r *Reader) Read(params url.Values, targets ...interface{}) error {
	var processed map[string]struct{}
	if r.strict {
		processed = make(map[string]struct{})
	}

	fieldErrors := multiError{}
	for _, target := range targets {
		targetVal := reflect.ValueOf(target)
		if targetVal.Kind() != reflect.Ptr {
			return errors.New("target must be a pointer")
		}

		targetVal = targetVal.Elem()
		if targetVal.Kind() != reflect.Struct {
			return errors.New("target must be a struct")
		}

		it := internal.NewIterator(targetVal, r.tag, r.mapper)
		for it.HasNext() {
			name, field := it.Next()
			if values, ok := params[name]; ok && len(values) > 0 {
				var err error

				if field.Kind() == reflect.Slice {
					err = r.readSlice(values, field)
				} else {
					err = r.readSingle(values, field, it)
				}

				if err != nil {
					fieldErrors[name] = err
				}

				if r.strict {
					processed[name] = struct{}{}
				}
			}
		}
	}

	if r.strict {
		for name := range params {
			if _, ok := processed[name]; !ok {
				fieldErrors[name] = errors.New("unknown parameter name")
			}
		}
	}

	if len(fieldErrors) > 0 {
		return fieldErrors
	}

	return nil
}

func (r *Reader) readSingle(values []string, field reflect.Value, it *internal.Iterator) error {
	if len(values) > 1 {
		return errors.New("multiple values for single value parameter")
	}

	// create empty field elements
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	parser, ok := internal.FindParser(field)
	if !ok {
		return errors.New("target field type is not supported")
	}
	if field.Kind() == reflect.Struct {
		it.SkipStruct()
	}

	err := parser.Parse(field, values[0])
	if err != nil {
		return err
	}

	return nil
}

func (r *Reader) readSlice(values []string, slice reflect.Value) error {
	slice.Set(reflect.MakeSlice(slice.Type(), len(values), len(values)))

	first := slice.Index(0)
	isPtr := first.Kind() == reflect.Ptr
	if isPtr {
		for i := 0; i < slice.Len(); i++ {
			elem := slice.Index(i)
			if elem.IsNil() {
				slice.Index(i).Set(reflect.New(slice.Index(i).Type().Elem()))
			}
		}
		first = first.Elem()
	}

	parser, ok := internal.FindParser(first)
	if !ok {
		return errors.New("target field type is not supported")
	}

	for i, value := range values {
		elem := slice.Index(i)
		if isPtr {
			elem = elem.Elem()
		}
		err := parser.Parse(elem, value)
		if err != nil {
			return err
		}
	}
	return nil
}

// MultiError is an error which also contains a map of additional (named) errors
// which altogether caused the actual failure.
type MultiError interface {
	error
	fmt.Formatter
	ErrorMap() map[string]error
}

// implementation of MultiError
type multiError map[string]error

// Error returns a string summarizing all errors
func (err multiError) Error() string {
	return fmt.Sprintf("%d errors occured while reading parameters", len(err))
}

// Format performs 'Printf' type formatting
func (err multiError) Format(f fmt.State, c rune) {
	format := "%"
	if width, ok := f.Width(); ok {
		format = format + strconv.Itoa(width)
	}
	if precision, ok := f.Precision(); ok {
		format = format + "." + strconv.Itoa(precision)
	}
	for _, flag := range []int{'+', '#'} {
		if f.Flag(flag) {
			format = format + fmt.Sprintf("%c", flag)
		}
	}

	format = fmt.Sprintf("%s%c", format, c)

	fmt.Println(format)

	formatted := fmt.Sprintf(format, err.ErrorMap())
	formatted = "multiError" + formatted[3:]

	f.Write([]byte(formatted))
}

// ErrorMap returns all field names with their respective errors
func (err multiError) ErrorMap() map[string]error {
	return err
}
