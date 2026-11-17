package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsolla-capstone-api/internal/store"
)

type bdStore struct {
	store.Store
	called           bool
	roles            []string
	keep, transferTo int64
}

func (f *bdStore) DeleteNonAdminUsers(_ context.Context, roles []string, keep, transferTo int64) (int, map[string]int, error) {
	f.called, f.roles, f.keep, f.transferTo = true, roles, keep, transferTo
	return 3, map[string]int{"student": 3}, nil
}

func TestDeleteAllUsers(t *testing.T) {
	cases := []struct {
		name      string
		role      string
		confirm   bool
		wantErr   error
		wantRoles string
	}{
		{"every non-admin", "", true, nil, ""},
		{"only students", "student", true, nil, "student"},
		{"only mentors", "mentor", true, nil, "mentor"},
		{"only lecturers", "lecturer", true, nil, "lecturer"},
		{"not confirmed: nothing happens", "", false, ErrDeleteUsersNotConfirmed, ""},
		{"admins cannot be bulk deleted", "admin", true, ErrDeleteUsersRole, ""},
		{"unknown role", "teacher", true, ErrDeleteUsersRole, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &bdStore{}
			res, err := NewUserService(st).DeleteAllUsers(context.Background(), 5, c.role, c.confirm)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if c.wantErr != nil {
				if st.called {
					t.Error("a refused bulk delete reached the store")
				}
				return
			}
			if strings.Join(st.roles, ",") != c.wantRoles || st.keep != 5 || st.transferTo != 5 || res.Deleted != 3 {
				t.Errorf("store got roles %v keep %d transfer %d; result %+v", st.roles, st.keep, st.transferTo, res)
			}
		})
	}
}
