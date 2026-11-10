package jsonscan

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	CodeInvalidValue    = "INVALID_VALUE"
	CodeInvalidEncoding = "INVALID_ENCODING"
	CodeTooLarge        = "TOO_LARGE"
	CodeTooSmall        = "TOO_SMALL"

	maxIssues = 20
)

type Issue struct {
	Field  string
	Code   string
	Params map[string]any
}

type frame struct {
	object    bool
	expectKey bool
	hasKey    bool
	key       string
	index     int
}

func Scan(data []byte) []Issue {
	s := scanner{data: data}
	s.run()
	return s.issues
}

type scanner struct {
	data   []byte
	pos    int
	stack  []frame
	issues []Issue
}

func (s *scanner) add(field, code string, params map[string]any) {
	if len(s.issues) < maxIssues {
		s.issues = append(s.issues, Issue{Field: field, Code: code, Params: params})
	}
}

func (s *scanner) run() {
	for s.pos < len(s.data) && len(s.issues) < maxIssues {
		switch s.data[s.pos] {
		case '{':
			s.stack = append(s.stack, frame{object: true, expectKey: true})
			s.pos++
		case '[':
			s.stack = append(s.stack, frame{})
			s.pos++
		case '}', ']':
			if len(s.stack) > 0 {
				s.stack = s.stack[:len(s.stack)-1]
			}
			s.pos++
		case ',':
			s.nextMember()
			s.pos++
		case ':':
			if n := len(s.stack); n > 0 {
				s.stack[n-1].expectKey = false
			}
			s.pos++
		case '"':
			if !s.scanString() {
				return
			}
		case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			s.scanNumber()
		default:
			s.pos++
		}
	}
}

func (s *scanner) nextMember() {
	n := len(s.stack)
	if n == 0 {
		return
	}
	top := &s.stack[n-1]
	if top.object {
		top.expectKey = true
		top.hasKey = false
		return
	}
	top.index++
}

func (s *scanner) path() string {
	var b strings.Builder
	for _, f := range s.stack {
		switch {
		case f.object && f.hasKey:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(f.key)
		case !f.object:
			b.WriteString("[" + strconv.Itoa(f.index) + "]")
		}
	}
	if b.Len() == 0 {
		return "request"
	}
	return b.String()
}

func (s *scanner) scanString() bool {
	start := s.pos
	i := start + 1
	var nul, badEncoding bool
	for {
		if i >= len(s.data) {
			s.pos = len(s.data)
			return false
		}
		c := s.data[i]
		if c == '"' {
			break
		}
		if c != '\\' {
			i++
			continue
		}
		if i+1 >= len(s.data) {
			s.pos = len(s.data)
			return false
		}
		switch s.data[i+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			i += 2
		case 'u':
			r, ok := hex4(s.data, i+2)
			if !ok {
				s.pos = len(s.data)
				return false
			}
			i += 6
			switch {
			case r == 0:
				nul = true
			case r >= 0xD800 && r <= 0xDBFF:
				if hasLowSurrogate(s.data, i) {
					i += 6
				} else {
					badEncoding = true
				}
			case r >= 0xDC00 && r <= 0xDFFF:
				badEncoding = true
			}
		default:
			s.pos = len(s.data)
			return false
		}
	}
	if !utf8.Valid(s.data[start+1 : i]) {
		badEncoding = true
	}
	if n := len(s.stack); n > 0 && s.stack[n-1].object && s.stack[n-1].expectKey {
		var key string
		if err := json.Unmarshal(s.data[start:i+1], &key); err != nil {
			key = string(s.data[start+1 : i])
		}
		s.stack[n-1].key = key
		s.stack[n-1].hasKey = true
	}
	field := s.path()
	if nul {
		s.add(field, CodeInvalidValue, nil)
	}
	if badEncoding {
		s.add(field, CodeInvalidEncoding, nil)
	}
	s.pos = i + 1
	return true
}

func (s *scanner) scanNumber() {
	start := s.pos
	i := start
	integer := true
	for i < len(s.data) {
		c := s.data[i]
		switch {
		case c == '.' || c == 'e' || c == 'E':
			integer = false
		case c == '-' || c == '+' || (c >= '0' && c <= '9'):
		default:
			s.finishNumber(start, i, integer)
			return
		}
		i++
	}
	s.finishNumber(start, i, integer)
}

func (s *scanner) finishNumber(start, end int, integer bool) {
	s.pos = end
	if !integer {
		return
	}
	_, err := strconv.ParseInt(string(s.data[start:end]), 10, 64)
	if err == nil || !errors.Is(err, strconv.ErrRange) {
		return
	}
	if s.data[start] == '-' {
		s.add(s.path(), CodeTooSmall, map[string]any{"min": int64(math.MinInt64)})
		return
	}
	s.add(s.path(), CodeTooLarge, map[string]any{"max": int64(math.MaxInt64)})
}

func hex4(data []byte, at int) (rune, bool) {
	if at+4 > len(data) {
		return 0, false
	}
	var r rune
	for _, c := range data[at : at+4] {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}

func hasLowSurrogate(data []byte, at int) bool {
	if at+6 > len(data) || data[at] != '\\' || data[at+1] != 'u' {
		return false
	}
	r, ok := hex4(data, at+2)
	return ok && r >= 0xDC00 && r <= 0xDFFF
}
