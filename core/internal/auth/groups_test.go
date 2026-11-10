package auth

import "testing"

func TestGroup_NamesReadsCommonName(t *testing.T) {
	names := Group_Names([]string{
		"CN=Архив,OU=Groups,DC=corp,DC=local",
		"cn=архив,OU=Other,DC=corp,DC=local",
		"Docs",
	})
	if len(names) != 2 || names[0] != "Архив" || names[1] != "Docs" {
		t.Fatalf("%v", names)
	}
}

func TestRoleIDs_ForGroupsKeepsChief(t *testing.T) {
	mapped := []GroupRole{{RoleID: "role-1", Group: "Архив"}}
	matched := RoleIDs_ForGroups([]string{"CN=Архив,OU=Groups,DC=corp,DC=local"}, mapped, []string{"role-user"}, "role-user")
	if len(matched) != 1 || matched[0] != "role-1" {
		t.Fatalf("matched %v", matched)
	}
	chiefMatched := RoleIDs_ForGroups([]string{"CN=Архив,OU=Groups,DC=corp,DC=local"}, mapped, []string{"role-chief"}, "role-user")
	if len(chiefMatched) != 1 || chiefMatched[0] != "role-chief" {
		t.Fatalf("chief matched %v", chiefMatched)
	}
	fallback := RoleIDs_ForGroups(nil, mapped, []string{"role-user"}, "role-user")
	if len(fallback) != 1 || fallback[0] != "role-user" {
		t.Fatalf("fallback %v", fallback)
	}
	if roleID, found := Matched_GroupRole([]string{"CN=Архив,OU=Groups,DC=corp,DC=local"}, mapped); !found || roleID != "role-1" {
		t.Fatalf("matched role %q %v", roleID, found)
	}
	if _, found := Matched_GroupRole([]string{"CN=Другая,OU=Groups,DC=corp,DC=local"}, mapped); found {
		t.Fatal("unknown group matched")
	}
	chiefFallback := RoleIDs_ForGroups(nil, mapped, []string{"role-chief"}, "role-user")
	if len(chiefFallback) != 1 || chiefFallback[0] != "role-chief" {
		t.Fatalf("chief fallback %v", chiefFallback)
	}
}
