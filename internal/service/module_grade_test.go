package service

import (
	"context"
	"errors"
	"testing"
)

// A missing or unknown week grade is the caller's mistake (ErrInvalidGrade, answered 422), not a server error.
func TestUpdateModuleGradeRejectsUnknownGrades(t *testing.T) {
	for _, grade := range []string{"", "purple", "GREEN", " red"} {
		_, err := NewSubmissionService(&subStore{}).UpdateModuleGrade(context.Background(), 1, []string{"admin"}, 4, grade)
		if !errors.Is(err, ErrInvalidGrade) {
			t.Errorf("grade %q: err = %v, want ErrInvalidGrade", grade, err)
		}
	}
}
