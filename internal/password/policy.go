package password

import "fmt"

func Validate(password string) error {
	if len(password) > 72 {
		return fmt.Errorf("password must be at most 72 bytes; use fewer characters if it contains symbols or non-English letters")
	}
	if len(password) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		hasLetter = hasLetter || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
		hasDigit = hasDigit || ('0' <= r && r <= '9')
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("password must include a letter and a number")
	}
	return nil
}
