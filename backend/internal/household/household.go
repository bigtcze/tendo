package household

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

// ErrNotFound means the household does not exist, the identifier is malformed,
// or the caller has no membership. The cases are deliberately indistinguishable.
var ErrNotFound = errors.New("household not found")

var errUnavailable = errors.New("household unavailable")

const readTimeout = 5 * time.Second

// Household is the read model of one household.
type Household struct {
	ID        string
	Name      string
	Timezone  string
	CreatedAt time.Time
	Version   int64
}

// Repository loads households visible to a member. FindForMember returns
// ErrNotFound when the household does not exist or the user is not a member;
// any other error is an infrastructure failure.
type Repository interface {
	FindForMember(ctx context.Context, userID, householdID string) (Household, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

// Get returns the household only when userID is a member. householdID is
// authorization context: malformed values are rejected before any persistence call.
func (s *Service) Get(ctx context.Context, userID, householdID string) (Household, error) {
	if !ids.IsCanonicalUUID(householdID) || !ids.IsCanonicalUUID(userID) {
		return Household{}, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	found, err := s.repository.FindForMember(ctx, userID, householdID)
	if errors.Is(err, ErrNotFound) {
		return Household{}, ErrNotFound
	}
	if err != nil {
		return Household{}, errUnavailable
	}
	return found, nil
}
