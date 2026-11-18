package service

import (
	"context"
	"testing"

	"xsolla-capstone-api/internal/store"
)

type lecturersStore struct {
	wgfStore
	modules []store.Module
}

func (f *lecturersStore) GetModulesForUser(ctx context.Context, userID int64, roles []string) ([]store.Module, error) {
	return append([]store.Module(nil), f.modules...), nil
}

// A student cannot read a module's enrollments, so each module they list carries its lecturers (id and name only).
func TestModulesCarryTheirLecturers(t *testing.T) {
	st := &lecturersStore{
		modules: []store.Module{{ID: 1, Title: "Week 1"}},
		wgfStore: wgfStore{enrollments: []store.Enrollment{
			{UserID: 4, Name: "Student", Email: "s@example.com", Roles: []string{"student"}},
			{UserID: 12, Name: "Leyla Aliyeva", Email: "l@example.com", Roles: []string{"lecturer"}},
		}},
	}
	got, err := NewModuleService(st).GetModulesForUser(context.Background(), 4, []string{"student"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Lecturers) != 1 || got[0].Lecturers[0].ID != 12 || got[0].Lecturers[0].Name != "Leyla Aliyeva" {
		t.Fatalf("lecturers = %+v", got)
	}
}
