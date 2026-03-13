package bencode

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
)

// Parse 将 bencode 字节流解码为通用 Go 值。
// 返回值只会使用 []byte、int64、[]any 和 map[string]any 这几种类型。
func Parse(data []byte) (any, error) {
	p := parser{data: data}
	value, err := p.readValue()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.data) {
		return nil, fmt.Errorf("unexpected trailing data at byte %d", p.pos)
	}
	return value, nil
}

// Decode 读取全部输入，并按 bencode 规则完成解码。
func Decode(r io.Reader) (any, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Marshal 将支持的 Go 值重新编码为规范化的 bencode 字节序列。
func Marshal(value any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) readValue() (any, error) {
	if p.pos >= len(p.data) {
		return nil, io.ErrUnexpectedEOF
	}

	switch p.data[p.pos] {
	case 'i':
		return p.readInteger()
	case 'l':
		return p.readList()
	case 'd':
		return p.readDict()
	default:
		if p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			return p.readBytes()
		}
		return nil, fmt.Errorf("unexpected token %q at byte %d", p.data[p.pos], p.pos)
	}
}

func (p *parser) readInteger() (int64, error) {
	p.pos++ // 吃掉整数起始标记 'i'
	start := p.pos
	for p.pos < len(p.data) && p.data[p.pos] != 'e' {
		p.pos++
	}
	if p.pos >= len(p.data) {
		return 0, io.ErrUnexpectedEOF
	}
	number, err := strconv.ParseInt(string(p.data[start:p.pos]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer at byte %d: %w", start, err)
	}
	p.pos++ // 吃掉整数结束标记 'e'
	return number, nil
}

func (p *parser) readList() ([]any, error) {
	p.pos++ // 吃掉列表起始标记 'l'
	var items []any
	for {
		if p.pos >= len(p.data) {
			return nil, io.ErrUnexpectedEOF
		}
		if p.data[p.pos] == 'e' {
			p.pos++
			return items, nil
		}
		item, err := p.readValue()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
}

func (p *parser) readDict() (map[string]any, error) {
	p.pos++ // 吃掉字典起始标记 'd'
	dict := make(map[string]any)
	for {
		if p.pos >= len(p.data) {
			return nil, io.ErrUnexpectedEOF
		}
		if p.data[p.pos] == 'e' {
			p.pos++
			return dict, nil
		}
		keyBytes, err := p.readBytes()
		if err != nil {
			return nil, err
		}
		value, err := p.readValue()
		if err != nil {
			return nil, err
		}
		dict[string(keyBytes)] = value
	}
}

func (p *parser) readBytes() ([]byte, error) {
	start := p.pos
	for p.pos < len(p.data) && p.data[p.pos] != ':' {
		if p.data[p.pos] < '0' || p.data[p.pos] > '9' {
			return nil, fmt.Errorf("invalid string length token %q at byte %d", p.data[p.pos], p.pos)
		}
		p.pos++
	}
	if p.pos >= len(p.data) {
		return nil, io.ErrUnexpectedEOF
	}
	length, err := strconv.Atoi(string(p.data[start:p.pos]))
	if err != nil {
		return nil, fmt.Errorf("invalid string length at byte %d: %w", start, err)
	}
	p.pos++ // 吃掉长度和内容之间的分隔符 ':'
	if p.pos+length > len(p.data) {
		return nil, io.ErrUnexpectedEOF
	}
	value := append([]byte(nil), p.data[p.pos:p.pos+length]...)
	p.pos += length
	return value, nil
}

func writeValue(buf *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case []byte:
		_, err := fmt.Fprintf(buf, "%d:", len(v))
		if err != nil {
			return err
		}
		_, err = buf.Write(v)
		return err
	case string:
		return writeValue(buf, []byte(v))
	case int:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case int8:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case int16:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case int32:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case int64:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case uint:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case uint8:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case uint16:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case uint32:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case uint64:
		_, err := fmt.Fprintf(buf, "i%de", v)
		return err
	case []any:
		if err := buf.WriteByte('l'); err != nil {
			return err
		}
		for _, item := range v {
			if err := writeValue(buf, item); err != nil {
				return err
			}
		}
		return buf.WriteByte('e')
	case map[string]any:
		if err := buf.WriteByte('d'); err != nil {
			return err
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := writeValue(buf, []byte(key)); err != nil {
				return err
			}
			if err := writeValue(buf, v[key]); err != nil {
				return err
			}
		}
		return buf.WriteByte('e')
	default:
		return fmt.Errorf("unsupported bencode type %T", value)
	}
}
