package service

import (
	"errors"
	"reflect"
	"testing"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

func TestTagServiceCreateTag(t *testing.T) {
	repo := &tagRepositoryMock{}
	svc := NewTagService(repo)

	tag, err := svc.CreateTag(5, CreateTagParams{Name: "Bug", Color: "#fff"})
	if err != nil {
		t.Fatalf("CreateTag error=%v", err)
	}
	if tag != repo.created || tag.SpaceID != 5 || tag.Name != "Bug" || tag.Color != "#fff" {
		t.Fatalf("tag=%+v created=%+v", tag, repo.created)
	}
}

func TestTagServiceCreateTagPreservesDuplicateErrorAndWrapsOtherErrors(t *testing.T) {
	repo := &tagRepositoryMock{createErr: domain.ErrTagNameTaken}
	svc := NewTagService(repo)
	if _, err := svc.CreateTag(1, CreateTagParams{Name: "x"}); !errors.Is(err, domain.ErrTagNameTaken) {
		t.Fatalf("duplicate error=%v", err)
	}

	wantErr := errors.New("db failed")
	repo = &tagRepositoryMock{createErr: wantErr}
	svc = NewTagService(repo)
	if _, err := svc.CreateTag(1, CreateTagParams{Name: "x"}); !errors.Is(err, wantErr) {
		t.Fatalf("wrapped error=%v", err)
	}
}

func TestTagServiceGetOrCreateByNameReturnsExisting(t *testing.T) {
	existing := &domain.Tag{ID: 9, SpaceID: 5, Name: "Bug"}
	repo := &tagRepositoryMock{tag: existing}
	svc := NewTagService(repo)

	got, err := svc.GetOrCreateByName(5, "Bug", "#fff")
	if err != nil || got != existing {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if repo.created != nil {
		t.Fatal("Create should not be called for existing tag")
	}
}

func TestTagServiceGetOrCreateByNameCreatesWhenMissing(t *testing.T) {
	repo := &tagRepositoryMock{byNameErr: gorm.ErrRecordNotFound}
	svc := NewTagService(repo)

	got, err := svc.GetOrCreateByName(5, "Bug", "#fff")
	if err != nil {
		t.Fatalf("error=%v", err)
	}
	if got != repo.created || got.SpaceID != 5 || got.Name != "Bug" || got.Color != "#fff" {
		t.Fatalf("got=%+v created=%+v", got, repo.created)
	}
}

func TestTagServiceGetOrCreateByNamePropagatesLookupAndCreateErrors(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	repo := &tagRepositoryMock{byNameErr: lookupErr}
	svc := NewTagService(repo)
	if _, err := svc.GetOrCreateByName(1, "x", "#fff"); !errors.Is(err, lookupErr) {
		t.Fatalf("lookup error=%v", err)
	}

	createErr := errors.New("create failed")
	repo = &tagRepositoryMock{byNameErr: gorm.ErrRecordNotFound, createErr: createErr}
	svc = NewTagService(repo)
	if _, err := svc.GetOrCreateByName(1, "x", "#fff"); !errors.Is(err, createErr) {
		t.Fatalf("create error=%v", err)
	}
}

func TestTagServiceGetAllBySpaceAddsUsage(t *testing.T) {
	tags := []domain.Tag{{ID: 1, SpaceID: 5, Name: "A"}, {ID: 2, SpaceID: 5, Name: "B"}}
	repo := &tagRepositoryMock{tags: tags, usageByID: map[uint]int64{1: 3, 2: 7}}
	svc := NewTagService(repo)

	got, err := svc.GetAllBySpace(5)
	if err != nil {
		t.Fatalf("GetAllBySpace error=%v", err)
	}
	want := []TagWithUsage{{Tag: tags[0], UsageCount: 3}, {Tag: tags[1], UsageCount: 7}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}

func TestTagServiceGetAllBySpaceStopsOnUsageError(t *testing.T) {
	wantErr := errors.New("count failed")
	repo := &tagRepositoryMock{tags: []domain.Tag{{ID: 1}}, usageErr: wantErr}
	svc := NewTagService(repo)
	if _, err := svc.GetAllBySpace(1); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
}

func TestTagServiceUpdateTagChecksSpaceAndChangesFields(t *testing.T) {
	tag := &domain.Tag{ID: 9, SpaceID: 5, Name: "Old", Color: "old"}
	repo := &tagRepositoryMock{tag: tag}
	svc := NewTagService(repo)
	name, color := "New", "new"

	got, err := svc.UpdateTag(5, 9, UpdateTagParams{Name: &name, Color: &color})
	if err != nil {
		t.Fatalf("UpdateTag error=%v", err)
	}
	if got.Name != "New" || got.Color != "new" || repo.updated != got {
		t.Fatalf("got=%+v updated=%+v", got, repo.updated)
	}
}

func TestTagServiceUpdateTagRejectsMissingAndWrongSpace(t *testing.T) {
	svc := NewTagService(&tagRepositoryMock{byIDErr: gorm.ErrRecordNotFound})
	if _, err := svc.UpdateTag(5, 9, UpdateTagParams{}); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("missing error=%v", err)
	}

	svc = NewTagService(&tagRepositoryMock{tag: &domain.Tag{ID: 9, SpaceID: 6}})
	if _, err := svc.UpdateTag(5, 9, UpdateTagParams{}); !errors.Is(err, ErrTagSpaceMismatch) {
		t.Fatalf("mismatch error=%v", err)
	}
}

func TestTagServiceUpdateTagPropagatesRepositoryErrors(t *testing.T) {
	wantErr := errors.New("update failed")
	repo := &tagRepositoryMock{tag: &domain.Tag{ID: 9, SpaceID: 5}, updateErr: wantErr}
	svc := NewTagService(repo)
	if _, err := svc.UpdateTag(5, 9, UpdateTagParams{}); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
}

func TestTagServiceDeleteTagChecksSpaceAndDeletes(t *testing.T) {
	repo := &tagRepositoryMock{tag: &domain.Tag{ID: 9, SpaceID: 5}}
	svc := NewTagService(repo)
	if err := svc.DeleteTag(5, 9); err != nil {
		t.Fatalf("DeleteTag error=%v", err)
	}
	if repo.deletedSpaceID != 5 || repo.deletedTagID != 9 {
		t.Fatalf("delete args=(%d,%d)", repo.deletedSpaceID, repo.deletedTagID)
	}

	svc = NewTagService(&tagRepositoryMock{tag: &domain.Tag{ID: 9, SpaceID: 6}})
	if err := svc.DeleteTag(5, 9); !errors.Is(err, ErrTagSpaceMismatch) {
		t.Fatalf("mismatch error=%v", err)
	}

	svc = NewTagService(&tagRepositoryMock{byIDErr: gorm.ErrRecordNotFound})
	if err := svc.DeleteTag(5, 9); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("not found error=%v", err)
	}
}

func TestTagServiceMergeTagsValidatesInputs(t *testing.T) {
	svc := NewTagService(&tagRepositoryMock{})
	if err := svc.MergeTags(1, 7, 7); !errors.Is(err, ErrCannotMergeSame) {
		t.Fatalf("same error=%v", err)
	}

	svc = NewTagService(&tagRepositoryMock{byIDErr: gorm.ErrRecordNotFound})
	if err := svc.MergeTags(1, 7, 8); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("missing source error=%v", err)
	}

	repo := &tagRepositoryMock{
		byID: map[uint]*domain.Tag{
			7: {ID: 7, SpaceID: 1},
		},
		byIDErr: gorm.ErrRecordNotFound,
	}
	svc = NewTagService(repo)
	if err := svc.MergeTags(1, 7, 8); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("missing target error=%v", err)
	}

	repo = &tagRepositoryMock{
		byID: map[uint]*domain.Tag{
			7: {ID: 7, SpaceID: 2},
			8: {ID: 8, SpaceID: 1},
		},
	}
	svc = NewTagService(repo)
	if err := svc.MergeTags(1, 7, 8); !errors.Is(err, ErrTagSpaceMismatch) {
		t.Fatalf("source mismatch error=%v", err)
	}
}

func TestTagServiceMergeTagsCallsRepository(t *testing.T) {
	repo := &tagRepositoryMock{
		byID: map[uint]*domain.Tag{
			7: {ID: 7, SpaceID: 1},
			8: {ID: 8, SpaceID: 1},
		},
	}
	svc := NewTagService(repo)

	if err := svc.MergeTags(1, 7, 8); err != nil {
		t.Fatalf("MergeTags error=%v", err)
	}
	if repo.mergedSpaceID != 1 || repo.mergedSourceID != 7 || repo.mergedTargetID != 8 {
		t.Fatalf("merge args=(%d,%d,%d)", repo.mergedSpaceID, repo.mergedSourceID, repo.mergedTargetID)
	}
}
