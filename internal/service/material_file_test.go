package service

import (
	"context"
	"errors"
	"testing"

	"xsolla-capstone-api/internal/store"
)

type mfStore struct {
	store.Store
	files map[int64]store.File
}

func (f *mfStore) GetFileByID(_ context.Context, id int64) (*store.File, error) {
	file, ok := f.files[id]
	if !ok {
		return nil, store.ErrFileNotFound
	}
	return &file, nil
}

func mfID(id int64) *int64 { return &id }

func TestMaterialFilesAreRestricted(t *testing.T) {
	svc := NewContentService(&mfStore{files: map[int64]store.File{
		1: {ID: 1, OriginalName: "week1.pdf", MimeType: "application/pdf"},
		2: {ID: 2, OriginalName: "slides.pptx", MimeType: "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		3: {ID: 3, OriginalName: "notes.md", MimeType: "text/markdown"},
		4: {ID: 4, OriginalName: "code.zip", MimeType: "application/zip"},
		5: {ID: 5, OriginalName: "essay.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		6: {ID: 6, OriginalName: "photo.png", MimeType: "image/png"},
	}})
	ctx := context.Background()
	for _, id := range []int64{1, 2, 3, 4} {
		if err := svc.checkMaterialFile(ctx, mfID(id)); err != nil {
			t.Errorf("file %d: %v, want allowed", id, err)
		}
	}
	for _, id := range []int64{5, 6, 99} {
		if err := svc.checkMaterialFile(ctx, mfID(id)); !errors.Is(err, ErrInvalidContentItem) {
			t.Errorf("file %d: %v, want refused", id, err)
		}
	}
	if err := svc.checkMaterialFile(ctx, nil); err != nil {
		t.Errorf("no file: %v", err)
	}
}

func TestMaterialsCarryTheirFileNameAndType(t *testing.T) {
	svc := NewContentService(&mfStore{files: map[int64]store.File{
		2: {ID: 2, OriginalName: "slides.pptx", MimeType: "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	}})
	items := []store.ContentItem{
		{ID: 10, Type: "file", Title: "Week 1 slides", FileID: mfID(2)},
		{ID: 11, Type: "link", Title: "Docs"},
		{ID: 12, Type: "file", Title: "Lost file", FileID: mfID(99)},
	}
	if err := svc.withFileInfo(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	if items[0].FileName == nil || *items[0].FileName != "slides.pptx" || items[0].FileMimeType == nil {
		t.Errorf("file material: %+v", items[0])
	}
	if items[1].FileName != nil || items[2].FileName != nil {
		t.Errorf("a link or a missing file must stay without a name: %+v %+v", items[1], items[2])
	}
}
