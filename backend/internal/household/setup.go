package household

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type ValidationError struct{ Field, Code string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Code }

type Bootstrap struct {
	Name     string
	Timezone string
}
type BootstrapRepository interface {
	CreateBootstrapHousehold(context.Context, Bootstrap) (string, error)
	AddOwner(context.Context, string, string) error
}
type BootstrapService struct{ repository BootstrapRepository }

func NewBootstrapService(repository BootstrapRepository) *BootstrapService {
	return &BootstrapService{repository: repository}
}
func (s *BootstrapService) CreateOwnerHousehold(ctx context.Context, input Bootstrap, userID string) (string, error) {
	name, err := Validate(input.Name, input.Timezone)
	if err != nil {
		return "", err
	}
	input.Name = name
	id, err := s.repository.CreateBootstrapHousehold(ctx, input)
	if err != nil {
		return "", err
	}
	if err = s.repository.AddOwner(ctx, userID, id); err != nil {
		return "", err
	}
	return id, nil
}
func Validate(name, timezone string) (string, error) {
	if !utf8.ValidString(name) {
		return "", &ValidationError{"householdName", "invalid_characters"}
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return "", &ValidationError{"householdName", "invalid_length"}
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", &ValidationError{"householdName", "invalid_characters"}
		}
	}
	if len(timezone) == 0 || len(timezone) > 128 || timezone == "Local" || strings.Contains(timezone, "..") || strings.HasPrefix(timezone, "/") || strings.HasPrefix(timezone, "+") || strings.HasPrefix(timezone, "-") {
		return "", &ValidationError{"timezone", "invalid_iana_timezone"}
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return "", &ValidationError{"timezone", "invalid_iana_timezone"}
	}
	return name, nil
}
