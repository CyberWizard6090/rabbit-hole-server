package integration

import (
	"net/http"
	"testing"
)

func assertPublicFormGone(t *testing.T, tc *TestContext, f uploadFixture, label string) {
	t.Helper()
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/public/forms/"+f.slug, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("%s: public form returned %d, want 404: %s", label, rec.Code, rec.Body.String())
	}
	if rec := f.submitWith(t, tc, "gone-"+label, f.plainAnswers()); rec.Code != http.StatusNotFound {
		t.Errorf("%s: public submission returned %d, want 404: %s", label, rec.Code, rec.Body.String())
	}
	if rec := f.upload(t, tc, "note.txt", []byte("hello")); rec.Code != http.StatusNotFound {
		t.Errorf("%s: public upload returned %d, want 404: %s", label, rec.Code, rec.Body.String())
	}
}

func assertPublicFormAlive(t *testing.T, tc *TestContext, f uploadFixture) {
	t.Helper()
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/public/forms/"+f.slug, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("public form before delete: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := f.upload(t, tc, "note.txt", []byte("hello")); rec.Code != http.StatusCreated {
		t.Fatalf("public upload before delete: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeadPublic_FormOfADeletedWorkspaceStopsWorking(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixtureWith(t, tc, "deadws", nil, false)
	assertPublicFormAlive(t, tc, f)

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/workspaces/"+itoa(f.owner.workspaceID), f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete workspace: %d: %s", rec.Code, rec.Body.String())
	}
	assertPublicFormGone(t, tc, f, "workspace")
}

func TestDeadPublic_FormOfADeletedWorkspaceStopsWorkingWithoutCascade(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixtureWith(t, tc, "deadwsraw", nil, false)
	assertPublicFormAlive(t, tc, f)

	if err := tc.DB.Exec("UPDATE workspaces SET deleted_at = now() WHERE id = ?", f.owner.workspaceID).Error; err != nil {
		t.Fatal(err)
	}
	assertPublicFormGone(t, tc, f, "workspace-raw")
}

func TestDeadPublic_FormWithADeletedTargetListStopsWorking(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixtureWith(t, tc, "deadlist", nil, false)
	assertPublicFormAlive(t, tc, f)

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/lists/"+itoa(f.listID), f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete list: %d: %s", rec.Code, rec.Body.String())
	}
	assertPublicFormGone(t, tc, f, "list")
}

func TestDeadPublic_FormWithATargetInADeletedSpaceStopsWorking(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixtureWith(t, tc, "deadspace", nil, false)
	assertPublicFormAlive(t, tc, f)

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/spaces/"+itoa(f.owner.spaceID), f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete space: %d: %s", rec.Code, rec.Body.String())
	}
	assertPublicFormGone(t, tc, f, "space")
}

func TestDeadPublic_FormWithATargetInADeletedFolderStopsWorkingWithoutCascade(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixtureWith(t, tc, "deadfolder", nil, false)
	assertPublicFormAlive(t, tc, f)

	folderID := mustCreate(t, tc, f.owner.token, "/api/v1/spaces/"+itoa(f.owner.spaceID)+"/folders", map[string]any{"name": "F"})
	if err := tc.DB.Exec("UPDATE lists SET folder_id = ? WHERE id = ?", folderID, f.listID).Error; err != nil {
		t.Fatal(err)
	}
	if err := tc.DB.Exec("UPDATE folders SET deleted_at = now() WHERE id = ?", folderID).Error; err != nil {
		t.Fatal(err)
	}
	assertPublicFormGone(t, tc, f, "folder-raw")
}

func TestDeadPublic_WorkspaceImagesOfADeletedWorkspaceAreNotServed(t *testing.T) {
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "deadimg")
	rec := uploadRequest(t, tc, "/api/v1/workspaces/"+itoa(owner.workspaceID)+"/images", owner.token, "logo.png", smallPNG(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload image: %d: %s", rec.Code, rec.Body.String())
	}
	url, _ := dataObject(t, rec)["url"].(string)
	if rec := request(t, tc.Router, http.MethodGet, url, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("image before delete: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/workspaces/"+itoa(owner.workspaceID), owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete workspace: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, url, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("image of a deleted workspace: %d, want 404", rec.Code)
	}
}

func TestDeadPublic_OwnerRoutesOfADeletedWorkspaceAreRefused(t *testing.T) {
	tc := newTestContext(t)
	f := newUploadFixtureWith(t, tc, "deadowner", nil, false)
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/workspaces/"+itoa(f.owner.workspaceID), f.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete workspace: %d", rec.Code)
	}
	for _, path := range []string{f.formURL, f.formURL + "/" + itoa(f.formID)} {
		if rec := request(t, tc.Router, http.MethodGet, path, f.owner.token, nil); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Errorf("GET %s after workspace delete: %d", path, rec.Code)
		}
	}
}
