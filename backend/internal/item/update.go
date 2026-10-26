package item

import (
	"context"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"strings"
)

type Patch struct {
	Title             *string
	SubjectID         *string
	ResponsibleUserID Nullable[string]
	Notes             Nullable[string]
	AttentionOn       Nullable[string]
	Recurrence        Nullable[schedule.Policy]
	WorkflowState     *string
	Archived          *bool
}

func (p Patch) empty() bool {
	return p.Title == nil && p.SubjectID == nil && !p.ResponsibleUserID.Set &&
		!p.Notes.Set && !p.AttentionOn.Set && !p.Recurrence.Set &&
		p.WorkflowState == nil && p.Archived == nil
}

type Change struct {
	Title             *string
	SubjectID         *string
	ResponsibleUserID Nullable[string]
	Notes             Nullable[string]
	AttentionOn       Nullable[schedule.Date]
	Recurrence        Nullable[schedule.Policy]
	WorkflowState     *WorkflowState
	Archived          *bool
}

func (s *Service) Update(ctx context.Context, userID, householdID, itemID string, expectedVersion int64, p Patch) (Item, error) {
	if !validUUID(itemID) {
		return Item{}, ErrNotFound
	}
	itemID = strings.ToLower(itemID)
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Item{}, err
	}
	if p.empty() {
		return Item{}, ErrEmptyPatch
	}
	change, err := s.buildScalarChanges(ctx, householdID, itemID, p)
	if err != nil {
		return Item{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	change, err = s.validateReferenceChanges(ctx, userID, householdID, itemID, p, change)
	if err != nil {
		return Item{}, err
	}
	updated, err := s.repository.Update(ctx, householdID, itemID, expectedVersion, change)
	if err != nil {
		return Item{}, mapRepoErr(err)
	}
	return derive(updated, today)
}

func (s *Service) buildScalarChanges(ctx context.Context, householdID, itemID string, p Patch) (Change, error) {
	var c Change
	fail := func(err error) (Change, error) {
		return Change{}, s.validationAfterExistenceCheck(ctx, householdID, itemID, err)
	}
	if p.Title != nil {
		title, err := ValidateTitle(*p.Title)
		if err != nil {
			return fail(err)
		}
		c.Title = &title
	}
	if p.Notes.Set {
		if p.Notes.Value != nil {
			if err := ValidateNotes(*p.Notes.Value); err != nil {
				return fail(err)
			}
		}
		c.Notes = p.Notes
	}
	if p.AttentionOn.Set {
		c.AttentionOn.Set = true
		if p.AttentionOn.Value != nil {
			d, err := ParseDate(*p.AttentionOn.Value)
			if err != nil {
				return fail(err)
			}
			c.AttentionOn.Value = &d
		}
	}
	if p.Recurrence.Set {
		if err := validateRecurrence(p.Recurrence.Value); err != nil {
			return fail(err)
		}
		p.Recurrence.Value = canonicalRecurrence(p.Recurrence.Value)
		c.Recurrence = p.Recurrence
	}
	if p.WorkflowState != nil {
		state, ok := ParseWorkflowState(*p.WorkflowState)
		if !ok {
			return fail(&ValidationError{"workflowState", "invalid_workflow_state"})
		}
		c.WorkflowState = &state
	}
	c.Archived = p.Archived
	return c, nil
}

func (s *Service) validateReferenceChanges(ctx context.Context, userID, householdID, itemID string, p Patch, c Change) (Change, error) {
	if p.SubjectID != nil {
		subjectID, err := s.checkSubject(ctx, userID, householdID, *p.SubjectID)
		if err != nil {
			return Change{}, s.validationAfterExistenceCheck(ctx, householdID, itemID, err)
		}
		c.SubjectID = &subjectID
	}
	if p.ResponsibleUserID.Set {
		c.ResponsibleUserID.Set = true
		if p.ResponsibleUserID.Value != nil {
			id, err := s.checkResponsibleMember(ctx, householdID, p.ResponsibleUserID.Value)
			if err != nil {
				return Change{}, s.validationAfterExistenceCheck(ctx, householdID, itemID, err)
			}
			c.ResponsibleUserID.Value = id
		}
	}
	return c, nil
}
