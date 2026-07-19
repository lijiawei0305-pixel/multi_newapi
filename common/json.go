package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func UnmarshalJsonStr(data string, v any) error {
	return json.Unmarshal(StringToByteSlice(data), v)
}

func DecodeJson(reader io.Reader, v any) error {
	return json.NewDecoder(reader).Decode(v)
}

// EncodeJson writes one JSON value and preserves encoding/json.Encoder's
// trailing newline semantics.
func EncodeJson(writer io.Writer, v any) error {
	return json.NewEncoder(writer).Encode(v)
}

// DecodeJsonWithLimit decodes one JSON value without first materializing the
// entire encoded body. It also consumes trailing input so whitespace or a
// second value cannot bypass the byte limit.
func DecodeJsonWithLimit(reader io.Reader, v any, limit int64) error {
	if reader == nil {
		return errors.New("limited JSON reader is nil")
	}
	if limit < 0 || limit == math.MaxInt64 {
		return errors.New("JSON read limit is invalid")
	}
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(v); err != nil {
		if limited.N == 0 {
			return &ReadLimitExceededError{Limit: limit}
		}
		return err
	}
	trailing := io.MultiReader(decoder.Buffered(), limited)
	var buffer [4096]byte
	for {
		read, err := trailing.Read(buffer[:])
		for _, value := range buffer[:read] {
			if value != ' ' && value != '\t' && value != '\r' && value != '\n' {
				return fmt.Errorf("JSON body contains multiple values")
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if limited.N == 0 {
		return &ReadLimitExceededError{Limit: limit}
	}
	return nil
}

func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// WriteJSONString emits one JSON string without allocating a second buffer the
// size of value. It matches encoding/json's default HTML-safe escaping.
func WriteJSONString(writer io.Writer, value string) error {
	if writer == nil {
		return errors.New("JSON string writer is nil")
	}
	if _, err := io.WriteString(writer, `"`); err != nil {
		return err
	}
	start := 0
	for index := 0; index < len(value); {
		r, size := utf8.DecodeRuneInString(value[index:])
		escaped := ""
		switch r {
		case '\\':
			escaped = `\\`
		case '"':
			escaped = `\"`
		case '\b':
			escaped = `\b`
		case '\f':
			escaped = `\f`
		case '\n':
			escaped = `\n`
		case '\r':
			escaped = `\r`
		case '\t':
			escaped = `\t`
		case '<':
			escaped = `\u003c`
		case '>':
			escaped = `\u003e`
		case '&':
			escaped = `\u0026`
		case '\u2028':
			escaped = `\u2028`
		case '\u2029':
			escaped = `\u2029`
		default:
			if r < 0x20 {
				escaped = fmt.Sprintf(`\u%04x`, r)
			} else if r == utf8.RuneError && size == 1 {
				escaped = `\ufffd`
			}
		}
		if escaped != "" {
			if _, err := io.WriteString(writer, value[start:index]); err != nil {
				return err
			}
			if _, err := io.WriteString(writer, escaped); err != nil {
				return err
			}
			start = index + size
		}
		index += size
	}
	if _, err := io.WriteString(writer, value[start:]); err != nil {
		return err
	}
	_, err := io.WriteString(writer, `"`)
	return err
}

func GetJsonType(data json.RawMessage) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "unknown"
	}
	firstChar := trimmed[0]
	switch firstChar {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

// JsonRawMessageToString returns JSON strings as their decoded value and other JSON values as raw text.
func JsonRawMessageToString(data json.RawMessage) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] != '"' {
		return string(trimmed)
	}
	var value string
	if err := Unmarshal(trimmed, &value); err != nil {
		return string(trimmed)
	}
	return value
}
