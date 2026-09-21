package keychain

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/programmablemike/localclaw/internal/domain"
)

// parsePassword reads the value from the stderr of find-generic-password
// -g. security prints `password: "..."` when every byte is printable ASCII
// without a backslash (the quoted form is not escaped, so the value ends
// at the last quote on the line), `password: 0x...` followed by a space for
// anything else, and `password: ` for an empty value.
func parsePassword(stderr []byte) ([]byte, error) {
	for _, line := range strings.Split(string(stderr), "\n") {
		rest, ok := strings.CutPrefix(line, "password: ")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(rest, "0x"):
			h := rest[2:]
			if i := strings.IndexByte(h, ' '); i >= 0 {
				h = h[:i]
			}
			v, err := hex.DecodeString(h)
			if err != nil {
				return nil, fmt.Errorf("decode hexadecimal password: %w", err)
			}
			return v, nil
		case strings.HasPrefix(rest, `"`):
			end := strings.LastIndexByte(rest, '"')
			if end == 0 {
				return nil, errors.New("unterminated quoted password")
			}
			return []byte(rest[1:end]), nil
		default:
			return []byte{}, nil
		}
	}
	return nil, errors.New("no password line in security output")
}

// stripPassword removes every password line so a value never reaches an
// error message.
func stripPassword(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "password:") {
			kept = append(kept, l)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// attrLine matches one attribute line of find-generic-password output:
//
//	"icmt"<blob>="user"
//	"cdat"<timedate>=0x32303236...  "20260921070345Z\000"
var attrLine = regexp.MustCompile(`^\s*"(\w+)"<\w+>=(.*)$`)

// parseAttributes reads the source and the timestamps. An item whose
// comment is not a known source was made by hand, so it reports "user".
func parseAttributes(stdout []byte) (domain.SecretItem, error) {
	item := domain.SecretItem{Source: domain.User}
	for _, line := range strings.Split(string(stdout), "\n") {
		m := attrLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		switch m[1] {
		case "icmt":
			if src, ok := domain.ParseSource(unquote(m[2])); ok {
				item.Source = src
			}
		case "cdat":
			t, err := parseTime(m[2])
			if err != nil {
				return item, fmt.Errorf("parse cdat: %w", err)
			}
			item.Created = t
		case "mdat":
			t, err := parseTime(m[2])
			if err != nil {
				return item, fmt.Errorf("parse mdat: %w", err)
			}
			item.Modified = t
		}
	}
	return item, nil
}

// unquote returns the content of a "..." blob, or "" for <NULL>.
func unquote(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s[1 : len(s)-1]
	}
	return ""
}

// parseTime reads the quoted rendering of a timedate attribute, such as
// "20260921070345Z\000", as UTC.
func parseTime(s string) (time.Time, error) {
	start := strings.IndexByte(s, '"')
	if start < 0 {
		return time.Time{}, fmt.Errorf("no quoted time in %q", s)
	}
	q := s[start+1:]
	if end := strings.IndexByte(q, '"'); end >= 0 {
		q = q[:end]
	}
	q = strings.TrimSuffix(strings.TrimSuffix(q, `\000`), "Z")
	return time.ParseInLocation("20060102150405", q, time.UTC)
}
