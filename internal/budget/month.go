package budget

// ValidateMonth accepts exactly YYYY-MM in years 0001–9999 and months 01–12.
// It does not consult a clock, timezone or calendar service.
func ValidateMonth(month string) error {
	if len(month) != 7 || month[4] != '-' ||
		!asciiDigits(month[:4]) || !asciiDigits(month[5:]) ||
		month[:4] == "0000" || month[5:] < "01" || month[5:] > "12" {
		return ErrInvalidMonth
	}
	return nil
}
