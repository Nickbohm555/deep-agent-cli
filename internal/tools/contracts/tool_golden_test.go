package contracts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const fixtureRepoDir = "fixtures/fixture_repo"

var fixtureRepoMu sync.Mutex

type readFileGolden struct {
	Success          string `json:"success"`
	MissingFileError string `json:"missing_file_error"`
}

type listFilesGolden struct {
	Success          string `json:"success"`
	MissingPathError string `json:"missing_path_error"`
}

type bashGolden struct {
	Success string `json:"success"`
	Failure string `json:"failure"`
}

type codeSearchGolden struct {
	Success             string `json:"success"`
	NoMatches           string `json:"no_matches"`
	MissingPatternError string `json:"missing_pattern_error"`
}

func TestReadFileGoldenGenerate(t *testing.T) {
	golden := readFileGolden{}
	withFixtureRepo(t, func() {
		result, err := legacyReadFile("README.md")
		if err != nil {
			t.Fatalf("read_file success: %v", err)
		}
		golden.Success = normalizeGoldenText(t, result)

		_, err = legacyReadFile("missing.txt")
		if err == nil {
			t.Fatal("read_file missing file: expected error")
		}
		golden.MissingFileError = normalizeGoldenText(t, err.Error())
	})

	writeOrCompareGolden(t, "testdata/read_file.golden", golden)
}

func TestListFilesGoldenGenerate(t *testing.T) {
	golden := listFilesGolden{}
	withFixtureRepo(t, func() {
		result, err := legacyListFiles(".")
		if err != nil {
			t.Fatalf("list_files success: %v", err)
		}
		golden.Success = normalizeGoldenText(t, result)

		_, err = legacyListFiles("missing-dir")
		if err == nil {
			t.Fatal("list_files missing path: expected error")
		}
		golden.MissingPathError = normalizeGoldenText(t, err.Error())
	})

	writeOrCompareGolden(t, "testdata/list_files.golden", golden)
}

func TestBashGoldenGenerate(t *testing.T) {
	golden := bashGolden{}
	withFixtureRepo(t, func() {
		result, err := legacyBash(`printf 'alpha-123-beta'`)
		if err != nil {
			t.Fatalf("bash success: %v", err)
		}
		golden.Success = normalizeGoldenText(t, result)

		result, err = legacyBash(`printf 'fail-line\n'; exit 7`)
		if err != nil {
			t.Fatalf("bash failure path should return formatted output, got error: %v", err)
		}
		golden.Failure = normalizeGoldenText(t, result)
	})

	writeOrCompareGolden(t, "testdata/bash.golden", golden)
}

func TestCodeSearchGoldenGenerate(t *testing.T) {
	golden := codeSearchGolden{}
	withFixtureRepo(t, func() {
		result, err := legacyCodeSearch(codeSearchInput{
			Pattern: `alpha-[0-9]+-beta`,
			Path:    ".",
		})
		if err != nil {
			t.Fatalf("code_search success: %v", err)
		}
		golden.Success = normalizeGoldenText(t, result)

		result, err = legacyCodeSearch(codeSearchInput{
			Pattern: `does-not-exist`,
			Path:    ".",
		})
		if err != nil {
			t.Fatalf("code_search no matches: %v", err)
		}
		golden.NoMatches = normalizeGoldenText(t, result)

		_, err = legacyCodeSearch(codeSearchInput{})
		if err == nil {
			t.Fatal("code_search missing pattern: expected error")
		}
		golden.MissingPatternError = normalizeGoldenText(t, err.Error())
	})

	writeOrCompareGolden(t, "testdata/code_search.golden", golden)
}

func withFixtureRepo(t *testing.T, fn func()) {
	t.Helper()

	fixtureRepoMu.Lock()
	defer fixtureRepoMu.Unlock()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	fixtureRoot := filepath.Join(wd, fixtureRepoDir)
	if err := os.Chdir(fixtureRoot); err != nil {
		t.Fatalf("chdir fixture repo: %v", err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	fn()
}

func normalizeGoldenText(t *testing.T, value string) string {
	t.Helper()

	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, `\`, `/`)

	fixtureRoot, err := os.Getwd()
	if err == nil {
		value = strings.ReplaceAll(value, fixtureRoot, "<FIXTURE_ROOT>")
		value = strings.ReplaceAll(value, filepath.ToSlash(fixtureRoot), "<FIXTURE_ROOT>")
	}

	return value
}

func writeOrCompareGolden(t *testing.T, path string, data any) {
	t.Helper()

	path = filepath.Join(contractsRootDir(t), path)

	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden %s: %v", path, err)
	}
	formatted = append(formatted, '\n')

	if os.Getenv("UPDATE_GOLDENS") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir for %s: %v", path, err)
		}
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}

	if string(current) != string(formatted) {
		t.Fatalf("golden mismatch for %s\nexpected:\n%s\nactual:\n%s", path, string(current), string(formatted))
	}
}

func contractsRootDir(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate contracts test file: runtime caller unavailable")
	}

	return filepath.Dir(filename)
}

func legacyReadFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func legacyListFiles(path string) (string, error) {
	dir := "."
	if path != "" {
		dir = path
	}

	cmd := exec.Command("find", dir, "-type", "f", "-not", "-path", "*/.devenv/*", "-not", "-path", "*/.git/*")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(files) == 1 && files[0] == "" {
		files = []string{}
	}

	result, err := json.Marshal(files)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

func legacyBash(command string) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Command failed with error: %s\nOutput: %s", err.Error(), string(output)), nil
	}

	return strings.TrimSpace(string(output)), nil
}

type codeSearchInput struct {
	Pattern       string
	Path          string
	FileType      string
	CaseSensitive bool
}

func legacyCodeSearch(input codeSearchInput) (string, error) {
	if input.Pattern == "" {
		return "", errors.New("pattern is required")
	}

	args := []string{"rg", "--line-number", "--with-filename", "--color=never"}
	if !input.CaseSensitive {
		args = append(args, "--ignore-case")
	}
	if input.FileType != "" {
		args = append(args, "--type", input.FileType)
	}

	args = append(args, input.Pattern)
	if input.Path != "" {
		args = append(args, input.Path)
	} else {
		args = append(args, ".")
	}

	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "No matches found", nil
		}
		return "", fmt.Errorf("search failed: %w", err)
	}

	result := strings.TrimSpace(string(output))
	lines := strings.Split(result, "\n")
	if len(lines) > 50 {
		result = strings.Join(lines[:50], "\n") + fmt.Sprintf("\n... (showing first 50 of %d matches)", len(lines))
	}

	return result, nil
}
