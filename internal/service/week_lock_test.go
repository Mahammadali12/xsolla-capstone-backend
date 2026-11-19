package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"xsolla-capstone-api/internal/store"
)

// A student gets nothing of a week that has not opened, whatever their browser's clock says; staff still see it.
func TestUnopenedWeekIsHiddenFromStudents(t *testing.T) {
	ctx := context.Background()
	future := time.Now().Add(48 * time.Hour)
	items := []store.ContentItem{{ID: 1, ModuleID: 4, Type: "file", Title: "Next week's notes", IsVisible: true}}

	st := &ooStore{items: items, opens: future, enrolled: true}
	got, err := NewContentService(st).GetContentItemsForModule(ctx, 31, []string{"student"}, 4)
	if err != nil || len(got) != 0 {
		t.Fatalf("student list = %v, %v; want an empty list", got, err)
	}
	if _, err := NewContentService(st).GetContentItemByID(ctx, 31, []string{"student"}, 1); !errors.Is(err, store.ErrNotEnrolled) {
		t.Fatalf("student item err = %v, want ErrNotEnrolled", err)
	}

	for _, roles := range [][]string{{"admin"}, {"mentor"}, {"lecturer"}} {
		got, err := NewContentService(st).GetContentItemsForModule(ctx, 31, roles, 4)
		if err != nil || len(got) != 1 {
			t.Fatalf("%v list = %v, %v; want the item", roles, got, err)
		}
	}

	open := &ooStore{items: items, opens: time.Now().Add(-time.Hour), enrolled: true}
	got, err = NewContentService(open).GetContentItemsForModule(ctx, 31, []string{"student"}, 4)
	if err != nil || len(got) != 1 {
		t.Fatalf("open week list = %v, %v; want the item", got, err)
	}
}

func TestUnopenedWeekQuizzesAreHiddenFromStudents(t *testing.T) {
	ctx := context.Background()
	st := newFakeQuizStore()
	st.isEnrolled = true
	st.modules[5] = &store.Module{ID: 5, OpensAt: time.Now().Add(48 * time.Hour), ClosesAt: time.Now().Add(96 * time.Hour)}
	st.quizzes[1] = &store.Quiz{ID: 1, ModuleID: 5, Title: "Next week's quiz", Status: "ready"}
	svc := NewQuizService(st, nil, nil)

	list, err := svc.ListQuizzesForModule(ctx, 99, []string{"student"}, 5)
	if err != nil || len(list) != 0 {
		t.Fatalf("student list = %v, %v; want an empty list", list, err)
	}
	if _, err := svc.GetQuiz(ctx, 99, []string{"student"}, 1); !errors.Is(err, store.ErrQuizNotFound) {
		t.Fatalf("student quiz err = %v, want ErrQuizNotFound", err)
	}
	if list, err := svc.ListQuizzesForModule(ctx, 1, []string{"admin"}, 5); err != nil || len(list) != 1 {
		t.Fatalf("admin list = %v, %v; want the quiz", list, err)
	}
}
