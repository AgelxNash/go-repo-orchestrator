package workdir

import "testing"

func TestManagedRepoDirKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		repoName string
		repoURL  string
		expected string
	}{
		{
			name:     "happy path keeps allowed symbols",
			repoName: "Repo.Name-1_2",
			repoURL:  "https://github.com/example/repo.git",
			expected: "Repo.Name-1_2__b7b426cf42606183",
		},
		{
			name:     "replaces special chars and trims edges",
			repoName: "__my repo/name??__",
			repoURL:  "https://gitlab.com/example/my-repo.git",
			expected: "my_repo_name__8428a82483bdb30a",
		},
		{
			name:     "falls back to repo when sanitized name empty",
			repoName: "!!!",
			repoURL:  "ssh://git@example.com/team/service.git",
			expected: "repo__f527c0330d97d58a",
		},
		{
			name:     "uses first eight sha256 bytes as hex suffix",
			repoName: "acme-repo",
			repoURL:  "https://github.com/acme/repo.git",
			expected: "acme-repo__51d5bdb516fa4d7c",
		},
		{
			name:     "falls back to repo and empty url hash on empty inputs",
			repoName: "",
			repoURL:  "",
			expected: "repo__e3b0c44298fc1c14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ManagedRepoDirKey(tt.repoName, tt.repoURL)
			if got != tt.expected {
				t.Fatalf("ManagedRepoDirKey(%q, %q) = %q, want %q", tt.repoName, tt.repoURL, got, tt.expected)
			}
		})
	}
}
