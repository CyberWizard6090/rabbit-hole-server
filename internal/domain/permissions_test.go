package domain

import (
	"regexp"
	"sort"
	"testing"

	migrations "rabbit-hole-server/db/migrations"
)

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPermissionCodesMatchSeedMigration(t *testing.T) {
	sql, err := migrations.FS.ReadFile("000002_seed_permissions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	var seeded []string
	for _, m := range regexp.MustCompile(`\('([a-z_.]+)'\)`).FindAllStringSubmatch(string(sql), -1) {
		seeded = append(seeded, m[1])
	}

	if got, want := sortedCopy(AllPermissions()), sortedCopy(seeded); !equalStrings(got, want) {
		t.Fatalf("AllPermissions() = %v\nseeded in migration = %v", got, want)
	}
}

func TestDefaultRolesOnlyUseKnownPermissionsAndOwnerHasAll(t *testing.T) {
	known := map[string]bool{}
	for _, code := range AllPermissions() {
		known[code] = true
	}

	for _, role := range DefaultRoles() {
		seen := map[string]bool{}
		for _, code := range role.PermissionCodes {
			if !known[code] {
				t.Errorf("role %s uses unknown permission %q", role.Name, code)
			}
			if seen[code] {
				t.Errorf("role %s lists %q twice", role.Name, code)
			}
			seen[code] = true
		}
		if role.Name == "Owner" {
			for code := range known {
				if !seen[code] {
					t.Errorf("Owner is missing permission %q", code)
				}
			}
		}
	}
}

func TestRoleMatrixPinsGuestAdminAndOwnerPrivileges(t *testing.T) {
	has := func(role, code string) bool {
		for _, r := range DefaultRoles() {
			if r.Name != role {
				continue
			}
			for _, c := range r.PermissionCodes {
				if c == code {
					return true
				}
			}
		}
		return false
	}
	formCodes := []string{PermFormRead, PermFormManage, PermTemplateRead, PermTemplateManage, PermFieldManage}
	for _, code := range formCodes {
		if has("Guest", code) {
			t.Errorf("Guest must not hold %s", code)
		}
		if !has("Admin", code) || !has("Owner", code) {
			t.Errorf("Admin and Owner must hold %s", code)
		}
	}
	for _, code := range []string{PermFormRead, PermTemplateRead} {
		if !has("Member", code) {
			t.Errorf("Member must hold %s", code)
		}
	}
	for _, code := range []string{PermFormManage, PermTemplateManage, PermFieldManage, PermAdminManage} {
		if has("Member", code) {
			t.Errorf("Member must not hold %s", code)
		}
	}
	if !has("Owner", PermAdminManage) || has("Admin", PermAdminManage) {
		t.Error("only the Owner may hold member.manage_admins")
	}
	if !has("Admin", PermWorkspaceUpdate) || has("Admin", PermWorkspaceDelete) {
		t.Error("Admin may edit the workspace but must not delete it")
	}
	if has("Member", PermWorkspaceUpdate) || has("Guest", PermWorkspaceUpdate) {
		t.Error("only Owner and Admin may edit the workspace")
	}
	if !has("Guest", PermWorkspaceRead) || !has("Guest", PermTaskRead) || has("Guest", PermTaskUpdate) {
		t.Error("Guest keeps read access to the work structure and nothing else")
	}
}
