package security

import (
	"reflect"
	"strings"
	"testing"
)

// Synthetic fixtures shaped like each class — none is a live credential.
var (
	fixturePrivateKey  = "-----BEGIN RSA PRIVATE KEY-----"
	fixtureAWSKey      = "AKIAIOSFODNN7EXAMPLE"
	fixtureGitHubToken = "ghp_" + strings.Repeat("a1", 18)
	fixtureGitHubPAT   = "github_pat_" + strings.Repeat("b2", 11)
	fixtureSlackToken  = "xoxb-0000000000-aaaaaaaaaa"
	fixtureJWT         = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.abcdefghijklmnop"
)

// addedLineDiff builds a one-file unified diff adding line to path.
func addedLineDiff(path, line string) string {
	return "diff --git a/" + path + " b/" + path + "\n" +
		"--- a/" + path + "\n" +
		"+++ b/" + path + "\n" +
		"@@ -0,0 +1 @@\n" +
		"+" + line + "\n"
}

func TestSecretFindingsMatchesEachClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		diff  string
		paths []string
		want  []SecretFinding
	}{
		{
			name: "private key",
			diff: addedLineDiff("config/server.go", `const key = "`+fixturePrivateKey+`"`),
			want: []SecretFinding{{Path: "config/server.go", Class: "private key"}},
		},
		{
			name: "AWS access key id",
			diff: addedLineDiff("deploy.sh", "export AWS_ACCESS_KEY_ID="+fixtureAWSKey),
			want: []SecretFinding{{Path: "deploy.sh", Class: "AWS access key id"}},
		},
		{
			name: "GitHub token",
			diff: addedLineDiff("ci.yml", "token: "+fixtureGitHubToken),
			want: []SecretFinding{{Path: "ci.yml", Class: "GitHub token"}},
		},
		{
			name: "GitHub fine-grained PAT",
			diff: addedLineDiff("ci.yml", "token: "+fixtureGitHubPAT),
			want: []SecretFinding{{Path: "ci.yml", Class: "GitHub token"}},
		},
		{
			name: "Slack token",
			diff: addedLineDiff("bot.py", `SLACK = "`+fixtureSlackToken+`"`),
			want: []SecretFinding{{Path: "bot.py", Class: "Slack token"}},
		},
		{
			name: "JWT",
			diff: addedLineDiff("fixtures.json", `{"auth": "`+fixtureJWT+`"}`),
			want: []SecretFinding{{Path: "fixtures.json", Class: "JWT"}},
		},
		{
			name: "removed line never matches",
			diff: "--- a/old.sh\n+++ b/old.sh\n@@ -1 +0,0 @@\n-export AWS_ACCESS_KEY_ID=" + fixtureAWSKey + "\n",
			want: nil,
		},
		{
			name: "the +++ header never matches",
			diff: "--- a/notes.txt\n+++ b/" + fixtureAWSKey + ".txt\n@@ -0,0 +1 @@\n+harmless\n",
			want: nil,
		},
		{
			name:  "glob finding precedes the content finding on the same file",
			diff:  addedLineDiff(".env", "AWS_ACCESS_KEY_ID="+fixtureAWSKey),
			paths: []string{".env"},
			want: []SecretFinding{
				{Path: ".env", Class: "secret-bearing file name"},
				{Path: ".env", Class: "AWS access key id"},
			},
		},
		{
			name: "one finding per (path, class), two files in diff order",
			diff: addedLineDiff("b.txt", fixtureSlackToken) +
				addedLineDiff("a.txt", fixtureSlackToken+" and again "+fixtureSlackToken) +
				"@@ -1 +2 @@\n+" + fixtureSlackToken + "\n",
			want: []SecretFinding{
				{Path: "b.txt", Class: "Slack token"},
				{Path: "a.txt", Class: "Slack token"},
			},
		},
		{
			name: "a quoted header path with spaces is attributed unquoted",
			diff: "--- a/x\n+++ \"b/my dir/caf\\303\\251.txt\"\n@@ -0,0 +1 @@\n+" + fixtureJWT + "\n",
			want: []SecretFinding{{Path: "my dir/café.txt", Class: "JWT"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := SecretFindings(tc.diff, tc.paths)

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SecretFindings = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSecretFindingsPathGlobs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want bool
	}{
		{"certs/server.pem", true},
		{"server.key", true},
		{"client.p12", true},
		{"client.pfx", true},
		{"keys/id_rsa", true},
		{"id_rsa_backup", true},
		{"id_ed25519", true},
		{"id_ecdsa", true},
		{".env", true},
		{"app/.env.local", true},
		{".env.production", true},
		{".env.example", false},
		{".env.sample", false},
		{".env.template", false},
		{"id_rsa.pub", false},
		{"id_ed25519.pub", false},
		{"certs/server.crt", false},
		{"main.go", false},
		{".env/README.md", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			got := SecretFindings("", []string{tc.path})

			flagged := len(got) == 1 && got[0] == SecretFinding{Path: tc.path, Class: "secret-bearing file name"}
			if flagged != tc.want {
				t.Errorf("SecretFindings(%q) = %v, want flagged=%v", tc.path, got, tc.want)
			}
		})
	}
}

func TestSecretsHintPinsTheWording(t *testing.T) {
	t.Parallel()

	finding := func(n int) []SecretFinding {
		fs := make([]SecretFinding, 0, n)
		for i := 0; i < n; i++ {
			fs = append(fs, SecretFinding{Path: string(rune('a' + i)), Class: "JWT"})
		}
		return fs
	}
	cases := []struct {
		name string
		fs   []SecretFinding
		want string
	}{
		{
			name: "none",
			fs:   nil,
			want: "",
		},
		{
			name: "one",
			fs:   []SecretFinding{{Path: ".env", Class: "secret-bearing file name"}},
			want: "staged secret material: .env (secret-bearing file name) — unstage it, or approve to commit anyway",
		},
		{
			name: "three",
			fs:   finding(3),
			want: "staged secret material: a (JWT), b (JWT), c (JWT) — unstage it, or approve to commit anyway",
		},
		{
			name: "five",
			fs:   finding(5),
			want: "staged secret material: a (JWT), b (JWT), c (JWT), +2 more — unstage it, or approve to commit anyway",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := SecretsHint(tc.fs)

			if got != tc.want {
				t.Errorf("SecretsHint = %q, want %q", got, tc.want)
			}
		})
	}
}
