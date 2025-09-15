package version

import (
	"testing"

	"k8s.io/apimachinery/pkg/version"
)

func TestGet(t *testing.T) {
	tests := []struct {
		name              string
		majorFromGit      string
		minorFromGit      string
		commitFromGit     string
		versionFromGit    string
		buildDate         string
		expectedMajor     string
		expectedMinor     string
		expectedCommit    string
		expectedVersion   string
		expectedBuildDate string
	}{
		{
			name:              "default empty values",
			majorFromGit:      "",
			minorFromGit:      "",
			commitFromGit:     "",
			versionFromGit:    "",
			buildDate:         "",
			expectedMajor:     "",
			expectedMinor:     "",
			expectedCommit:    "",
			expectedVersion:   "",
			expectedBuildDate: "",
		},
		{
			name:              "with version information",
			majorFromGit:      "1",
			minorFromGit:      "2",
			commitFromGit:     "abc123def456",
			versionFromGit:    "v1.2.3",
			buildDate:         "2023-01-01T12:00:00Z",
			expectedMajor:     "1",
			expectedMinor:     "2",
			expectedCommit:    "abc123def456",
			expectedVersion:   "v1.2.3",
			expectedBuildDate: "2023-01-01T12:00:00Z",
		},
		{
			name:              "partial version information",
			majorFromGit:      "2",
			minorFromGit:      "",
			commitFromGit:     "short",
			versionFromGit:    "",
			buildDate:         "2024-06-15T09:30:45Z",
			expectedMajor:     "2",
			expectedMinor:     "",
			expectedCommit:    "short",
			expectedVersion:   "",
			expectedBuildDate: "2024-06-15T09:30:45Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original values
			origMajor := majorFromGit
			origMinor := minorFromGit
			origCommit := commitFromGit
			origVersion := versionFromGit
			origBuildDate := buildDate

			// Set test values
			majorFromGit = tt.majorFromGit
			minorFromGit = tt.minorFromGit
			commitFromGit = tt.commitFromGit
			versionFromGit = tt.versionFromGit
			buildDate = tt.buildDate

			// Test Get function
			info := Get()

			// Verify results
			if info.Major != tt.expectedMajor {
				t.Errorf("Get().Major = %v, want %v", info.Major, tt.expectedMajor)
			}
			if info.Minor != tt.expectedMinor {
				t.Errorf("Get().Minor = %v, want %v", info.Minor, tt.expectedMinor)
			}
			if info.GitCommit != tt.expectedCommit {
				t.Errorf("Get().GitCommit = %v, want %v", info.GitCommit, tt.expectedCommit)
			}
			if info.GitVersion != tt.expectedVersion {
				t.Errorf("Get().GitVersion = %v, want %v", info.GitVersion, tt.expectedVersion)
			}
			if info.BuildDate != tt.expectedBuildDate {
				t.Errorf("Get().BuildDate = %v, want %v", info.BuildDate, tt.expectedBuildDate)
			}

			// Restore original values
			majorFromGit = origMajor
			minorFromGit = origMinor
			commitFromGit = origCommit
			versionFromGit = origVersion
			buildDate = origBuildDate
		})
	}
}

func TestGetReturnType(t *testing.T) {
	info := Get()

	// Verify that Get returns the correct type
	if _, ok := interface{}(info).(version.Info); !ok {
		t.Error("Get() should return version.Info type")
	}
}

func TestGetConsistency(t *testing.T) {
	// Test that multiple calls to Get() return consistent results
	info1 := Get()
	info2 := Get()

	if info1.Major != info2.Major {
		t.Error("Get() calls should return consistent Major version")
	}
	if info1.Minor != info2.Minor {
		t.Error("Get() calls should return consistent Minor version")
	}
	if info1.GitCommit != info2.GitCommit {
		t.Error("Get() calls should return consistent GitCommit")
	}
	if info1.GitVersion != info2.GitVersion {
		t.Error("Get() calls should return consistent GitVersion")
	}
	if info1.BuildDate != info2.BuildDate {
		t.Error("Get() calls should return consistent BuildDate")
	}
}

func TestGetFieldsNotNil(t *testing.T) {
	info := Get()

	// All fields should be strings (not nil), even if empty
	if info.Major == "" && majorFromGit != "" {
		t.Error("Major field should match majorFromGit")
	}
	if info.Minor == "" && minorFromGit != "" {
		t.Error("Minor field should match minorFromGit")
	}
	if info.GitCommit == "" && commitFromGit != "" {
		t.Error("GitCommit field should match commitFromGit")
	}
	if info.GitVersion == "" && versionFromGit != "" {
		t.Error("GitVersion field should match versionFromGit")
	}
	if info.BuildDate == "" && buildDate != "" {
		t.Error("BuildDate field should match buildDate")
	}
}
