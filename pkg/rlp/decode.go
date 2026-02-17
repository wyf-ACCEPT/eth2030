package rlp

import (
	"bytes"
	"io"
	"math/big"
	"reflect"
)

// Kind represents the type of an RLP value.
type Kind int

const (
	Byte   Kind = iota // Single byte in [0x00, 0x7f].
	String             // RLP string (including empty string).
	List               // RLP list.
)

// Decode reads an RLP-encoded value from r and stores it in the value pointed to by val.
func Decode(r io.Reader, val interface{}) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return DecodeBytes(data, val)
}

// DecodeBytes decodes an RLP-encoded byte slice into the value pointed to by val.
func DecodeBytes(b []byte, val interface{}) error {
	s := newByteStream(b)
	err := s.decodeValue(reflect.ValueOf(val))
	if err != nil {
		return err
	}
	return nil
}

// Stream provides streaming access to RLP-encoded data.
type Stream struct {
	data  []byte
	pos   int
	stack []listFrame // for List/ListEnd scoping
}

type listFrame struct {
	end int // exclusive end position of the current list
}

// NewStream creates a new RLP stream reading from r.
func NewStream(r io.Reader) *Stream {
	data, _ := io.ReadAll(r)
	return newByteStream(data)
}

func newByteStream(data []byte) *Stream {
	return &Stream{data: data, pos: 0}
}

// Kind reads the RLP type tag and content size of the next value without consuming it.
func (s *Stream) Kind() (Kind, uint64, error) {
	lim := s.limit()
	if s.pos >= lim {
		return 0, 0, io.EOF
	}
	prefix := s.data[s.pos]
	switch {
	case prefix <= 0x7f:
		return Byte, 1, nil
	case prefix <= 0xb7:
		return String, uint64(prefix - 0x80), nil
	case prefix <= 0xbf:
		lenOfLen := int(prefix - 0xb7)
		if s.pos+1+lenOfLen > lim {
			return 0, 0, io.ErrUnexpectedEOF
		}
		size := readBigEndian(s.data[s.pos+1 : s.pos+1+lenOfLen])
		return String, size, nil
	case prefix <= 0xf7:
		return List, uint64(prefix - 0xc0), nil
	default:
		lenOfLen := int(prefix - 0xf7)
		if s.pos+1+lenOfLen > lim {
			return 0, 0, io.ErrUnexpectedEOF
		}
		size := readBigEndian(s.data[s.pos+1 : s.pos+1+lenOfLen])
		return List, size, nil
	}
}

// readItem reads a complete RLP item (prefix + payload) and returns the payload bytes
// and the total number of bytes consumed. For single bytes [0x00, 0x7f], the payload
// is the byte itself.
func (s *Stream) readItem() (kind Kind, payload []byte, totalConsumed int, err error) {
	lim := s.limit()
	if s.pos >= lim {
		return 0, nil, 0, io.EOF
	}
	prefix := s.data[s.pos]

	switch {
	case prefix <= 0x7f:
		// Single byte.
		payload = s.data[s.pos : s.pos+1]
		s.pos++
		return Byte, payload, 1, nil

	case prefix <= 0xb7:
		// Short string: 0-55 bytes.
		size := int(prefix - 0x80)
		start := s.pos + 1
		end := start + size
		if end > lim {
			return 0, nil, 0, io.ErrUnexpectedEOF
		}
		if size == 1 && s.data[start] <= 0x7f {
			return 0, nil, 0, ErrCanonSize
		}
		payload = s.data[start:end]
		total := 1 + size
		s.pos = end
		return String, payload, total, nil

	case prefix <= 0xbf:
		// Long string.
		lenOfLen := int(prefix - 0xb7)
		if s.pos+1+lenOfLen > lim {
			return 0, nil, 0, io.ErrUnexpectedEOF
		}
		sizeBytes := s.data[s.pos+1 : s.pos+1+lenOfLen]
		if len(sizeBytes) > 0 && sizeBytes[0] == 0 {
			return 0, nil, 0, ErrCanonInt
		}
		size := int(readBigEndian(sizeBytes))
		if size <= 55 {
			return 0, nil, 0, ErrNonCanonicalSize
		}
		start := s.pos + 1 + lenOfLen
		end := start + size
		if end > lim {
			return 0, nil, 0, io.ErrUnexpectedEOF
		}
		payload = s.data[start:end]
		total := 1 + lenOfLen + size
		s.pos = end
		return String, payload, total, nil

	case prefix <= 0xf7:
		// Short list.
		size := int(prefix - 0xc0)
		start := s.pos + 1
		end := start + size
		if end > lim {
			return 0, nil, 0, io.ErrUnexpectedEOF
		}
		payload = s.data[start:end]
		total := 1 + size
		s.pos = end
		return List, payload, total, nil

	default:
		// Long list.
		lenOfLen := int(prefix - 0xf7)
		if s.pos+1+lenOfLen > lim {
			return 0, nil, 0, io.ErrUnexpectedEOF
		}
		sizeBytes := s.data[s.pos+1 : s.pos+1+lenOfLen]
		if len(sizeBytes) > 0 && sizeBytes[0] == 0 {
			return 0, nil, 0, ErrCanonInt
		}
		size := int(readBigEndian(sizeBytes))
		if size <= 55 {
			return 0, nil, 0, ErrNonCanonicalSize
		}
		start := s.pos + 1 + lenOfLen
		end := start + size
		if end > lim {
			return 0, nil, 0, io.ErrUnexpectedEOF
		}
		payload = s.data[start:end]
		total := 1 + lenOfLen + size
		s.pos = end
		return List, payload, total, nil
	}
}

// Bytes reads an RLP string value and returns it as []byte.
func (s *Stream) Bytes() ([]byte, error) {
	kind, payload, _, err := s.readItem()
	if err != nil {
		return nil, err
	}
	switch kind {
	case Byte, String:
		return payload, nil
	default:
		return nil, ErrExpectedString
	}
}

// List reads the start of an RLP list and enters a scope for reading list items.
// Subsequent Bytes/Uint64/etc. calls read from within the list. Call ListEnd
// when done reading.
func (s *Stream) List() (uint64, error) {
	if s.pos >= s.limit() {
		return 0, io.EOF
	}
	prefix := s.data[s.pos]

	var payloadStart, payloadEnd int
	switch {
	case prefix >= 0xc0 && prefix <= 0xf7:
		size := int(prefix - 0xc0)
		payloadStart = s.pos + 1
		payloadEnd = payloadStart + size
	case prefix > 0xf7:
		lenOfLen := int(prefix - 0xf7)
		if s.pos+1+lenOfLen > s.limit() {
			return 0, io.ErrUnexpectedEOF
		}
		sizeBytes := s.data[s.pos+1 : s.pos+1+lenOfLen]
		if len(sizeBytes) > 0 && sizeBytes[0] == 0 {
			return 0, ErrCanonInt
		}
		size := int(readBigEndian(sizeBytes))
		if size <= 55 {
			return 0, ErrNonCanonicalSize
		}
		payloadStart = s.pos + 1 + lenOfLen
		payloadEnd = payloadStart + size
	default:
		return 0, ErrExpectedList
	}

	if payloadEnd > s.limit() {
		return 0, io.ErrUnexpectedEOF
	}
	s.stack = append(s.stack, listFrame{end: payloadEnd})
	s.pos = payloadStart
	return uint64(payloadEnd - payloadStart), nil
}

// ListEnd verifies that all items in the current list have been read.
func (s *Stream) ListEnd() error {
	if len(s.stack) == 0 {
		return ErrExpectedList
	}
	top := s.stack[len(s.stack)-1]
	if s.pos != top.end {
		return ErrEOL
	}
	s.stack = s.stack[:len(s.stack)-1]
	return nil
}

// limit returns the current read boundary.
func (s *Stream) limit() int {
	if len(s.stack) > 0 {
		return s.stack[len(s.stack)-1].end
	}
	return len(s.data)
}

// Uint64 reads an RLP-encoded unsigned integer.
func (s *Stream) Uint64() (uint64, error) {
	b, err := s.Bytes()
	if err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, nil
	}
	if len(b) > 8 {
		return 0, ErrUint64Range
	}
	if len(b) > 1 && b[0] == 0 {
		return 0, ErrCanonInt
	}
	var val uint64
	for _, x := range b {
		val = (val << 8) | uint64(x)
	}
	return val, nil
}

// BigInt reads an RLP-encoded big integer.
func (s *Stream) BigInt() (*big.Int, error) {
	b, err := s.Bytes()
	if err != nil {
		return nil, err
	}
	if len(b) > 1 && b[0] == 0 {
		return nil, ErrCanonInt
	}
	i := new(big.Int).SetBytes(b)
	return i, nil
}

// peekItem returns the kind without consuming.
func (s *Stream) peekItem() (Kind, []byte, int, error) {
	saved := s.pos
	k, p, t, err := s.readItem()
	s.pos = saved
	return k, p, t, err
}

func readBigEndian(b []byte) uint64 {
	var val uint64
	for _, x := range b {
		val = (val << 8) | uint64(x)
	}
	return val
}

// decodeValue decodes the next RLP value into v (must be a pointer).
func (s *Stream) decodeValue(v reflect.Value) error {
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return ErrExpectedString
	}
	return s.decodeInto(v.Elem())
}

func (s *Stream) decodeInto(v reflect.Value) error {
	// Handle *big.Int specially.
	if v.Type() == reflect.TypeOf(big.Int{}) {
		bi, err := s.BigInt()
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(*bi))
		return nil
	}
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		if v.Type() == reflect.TypeOf((*big.Int)(nil)) {
			bi, err := s.BigInt()
			if err != nil {
				return err
			}
			v.Set(reflect.ValueOf(bi))
			return nil
		}
		return s.decodeInto(v.Elem())
	}

	switch v.Kind() {
	case reflect.Bool:
		b, err := s.Bytes()
		if err != nil {
			return err
		}
		if len(b) == 0 {
			v.SetBool(false)
		} else if len(b) == 1 && b[0] == 0x01 {
			v.SetBool(true)
		} else if len(b) == 1 && b[0] == 0x00 {
			v.SetBool(false)
		} else {
			return ErrCanonInt
		}
		return nil

	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint:
		u, err := s.Uint64()
		if err != nil {
			return err
		}
		v.SetUint(u)
		return nil

	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		u, err := s.Uint64()
		if err != nil {
			return err
		}
		v.SetInt(int64(u))
		return nil

	case reflect.String:
		b, err := s.Bytes()
		if err != nil {
			return err
		}
		v.SetString(string(b))
		return nil

	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			b, err := s.Bytes()
			if err != nil {
				return err
			}
			v.SetBytes(bytes.Clone(b))
			return nil
		}
		return s.decodeList(v)

	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			b, err := s.Bytes()
			if err != nil {
				return err
			}
			for i := 0; i < v.Len() && i < len(b); i++ {
				v.Index(i).SetUint(uint64(b[i]))
			}
			return nil
		}
		return s.decodeList(v)

	case reflect.Struct:
		return s.decodeStruct(v)

	default:
		return ErrExpectedString
	}
}

func (s *Stream) decodeList(v reflect.Value) error {
	_, err := s.List()
	if err != nil {
		return err
	}

	isSlice := v.Kind() == reflect.Slice
	i := 0
	for s.pos < s.stack[len(s.stack)-1].end {
		if isSlice {
			if i >= v.Len() {
				v.Set(reflect.Append(v, reflect.New(v.Type().Elem()).Elem()))
			}
		}
		if i < v.Len() {
			if err := s.decodeInto(v.Index(i)); err != nil {
				return err
			}
		}
		i++
	}
	return s.ListEnd()
}

func (s *Stream) decodeStruct(v reflect.Value) error {
	_, err := s.List()
	if err != nil {
		return err
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if err := s.decodeInto(v.Field(i)); err != nil {
			return err
		}
	}
	return s.ListEnd()
}
