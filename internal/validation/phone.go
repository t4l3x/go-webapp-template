package validation

import (
	"errors"
	"regexp"
	"strings"
)

// e164Pattern matches the E.164 phone number format: a leading '+',
// followed by 2-15 digits with no leading zero.
var e164Pattern = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// phoneSeparators strips the human formatting characters that carry no
// meaning in E.164, so "+1 415-555-0100" and "+14155550100" normalize
// to the same stored value. A *strings.Replacer is safe for concurrent
// use, so it is built once rather than per call.
var phoneSeparators = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")

// NormalizeE164Phone strips those separators and checks the result is
// syntactically valid E.164. E.164 is a published numbering-plan
// format, not a project policy — whether a phone number is required at
// all, or must be verified before use, stays with the module that
// decides it.
func NormalizeE164Phone(raw string) (string, error) {
	phone := phoneSeparators.Replace(strings.TrimSpace(raw))

	if !e164Pattern.MatchString(phone) {
		return "", errors.New("phone is not in E.164 format")
	}

	return phone, nil
}
