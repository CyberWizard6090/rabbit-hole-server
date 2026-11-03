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
