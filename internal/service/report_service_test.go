package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsolla-capstone-api/internal/store"
)

type rptStore struct {
	reportStore
	created          []string
	filter           store.ReportFilter
	status, note     *string
	updated, deleted int64
}

func (f *rptStore) CreateReport(_ context.Context, reporterID int64, subject, message string) (*store.Report, error) {
	f.created = append(f.created, subject, message)
	return &store.Report{ID: 1, ReporterID: &reporterID, Subject: subject, Message: message, Status: ReportOpen}, nil
}

func (f *rptStore) ListReports(_ context.Context, filter store.ReportFilter) ([]store.Report, error) {
	f.filter = filter
	return []store.Report{}, nil
}

func (f *rptStore) UpdateReport(_ context.Context, id int64, status, note *string, _ int64) (*store.Report, error) {
	f.updated, f.status, f.note = id, status, note
	return &store.Report{ID: id}, nil
}

func rptFields(err error) []string {
	var invalid *ReportInvalidError
	if !errors.As(err, &invalid) {
		return nil
	}
	out := []string{}
	for _, f := range invalid.Fields {
		out = append(out, f.Field)
	}
	return out
}

func TestSendReport(t *testing.T) {
	st := &rptStore{}
	svc := NewReportService(st)
	r, err := svc.Send(context.Background(), 7, "  Student X is rude in class ", "\nHe interrupts others every lecture.  ")
	if err != nil || r.Subject != "Student X is rude in class" || st.created[1] != "He interrupts others every lecture." {
		t.Fatalf("send: %+v, %v, stored %q", r, err, st.created)
	}

	cases := []struct {
		name, subject, message string
		want                   []string
	}{
		{"both empty", "", "  ", []string{"subject", "message"}},
		{"subject too long", strings.Repeat("a", 201), "fine", []string{"subject"}},
		{"200 letters (not bytes) are fine", strings.Repeat("ə", 200), "fine", nil},
		{"message too long", "fine", strings.Repeat("a", 5001), []string{"message"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewReportService(&rptStore{}).Send(context.Background(), 7, c.subject, c.message)
			if got := rptFields(err); strings.Join(got, ",") != strings.Join(c.want, ",") || (c.want == nil) != (err == nil) {
				t.Errorf("fields %v (err %v), want %v", got, err, c.want)
			}
		})
	}
}

func TestListReportFilters(t *testing.T) {
	st := &rptStore{}
	if _, err := NewReportService(st).List(context.Background(), store.ReportFilter{Status: "open", Role: "mentor", Query: "  rude "}); err != nil {
		t.Fatal(err)
	}
	if st.filter.Query != "rude" || st.filter.Role != "mentor" {
		t.Errorf("store got %+v", st.filter)
	}
	_, err := NewReportService(st).List(context.Background(), store.ReportFilter{Status: "closed", Role: "parent"})
	if got := rptFields(err); strings.Join(got, ",") != "status,role" {
		t.Errorf("bad filter: %v", got)
	}
}

func TestUpdateReport(t *testing.T) {
	str := func(s string) *string { return &s }
	st := &rptStore{}
	if _, err := NewReportService(st).Update(context.Background(), 1, 9, str("resolved"), str("  Talked to him.  ")); err != nil {
		t.Fatal(err)
	}
	if st.updated != 9 || *st.status != "resolved" || *st.note != "Talked to him." {
		t.Errorf("store got id %d status %v note %v", st.updated, st.status, st.note)
	}

	cases := []struct {
		name         string
		status, note *string
		want         string
	}{
		{"nothing to change", nil, nil, "status"},
		{"unknown status", str("closed"), nil, "status"},
		{"note too long", nil, str(strings.Repeat("a", 2001)), "admin_note"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &rptStore{}
			_, err := NewReportService(st).Update(context.Background(), 1, 9, c.status, c.note)
			if got := rptFields(err); strings.Join(got, ",") != c.want || st.updated != 0 {
				t.Errorf("fields %v, reached the store: %v", got, st.updated != 0)
			}
		})
	}
	// an empty note is allowed: it removes the note
	if _, err := NewReportService(st).Update(context.Background(), 1, 9, nil, str("  ")); err != nil || *st.note != "" {
		t.Errorf("clearing the note: %v, store got %q", err, *st.note)
	}
}
