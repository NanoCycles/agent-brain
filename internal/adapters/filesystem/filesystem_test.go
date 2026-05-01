package filesystem

import "testing"

func TestIsForbiddenPath(t *testing.T) {
	cases := []string{
		".git/config",
		"vendor/pkg/a.go",
		".env",
		".env.local",
		"certs/prod.pem",
		"keys/app.key",
		"credentials/db.yml",
		"secrets/token.txt",
		"home/id_rsa",
	}
	for _, tc := range cases {
		if !IsForbiddenPath(tc) {
			t.Fatalf("expected forbidden path: %s", tc)
		}
	}
	if IsForbiddenPath("internal/domain/user.go") {
		t.Fatal("safe Go source path was marked forbidden")
	}
}
