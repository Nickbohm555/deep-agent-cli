package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileContract(t *testing.T) {
	t.Parallel()

	var golden readFileGolden
	readGoldenFile(t, "testdata/read_file.golden", &golden)

	t.Run("success", func(t *testing.T) {
		withFixtureRepo(t, func() {
			result, err := legacyReadFile("README.md")
			if err != nil {
				t.Fatalf("read_file success: %v", err)
			}
			assertContractOutput(t, normalizeGoldenText(t, result), golden.Success)
		})
	})

	t.Run("missing_file", func(t *testing.T) {
		withFixtureRepo(t, func() {
			_, err := legacyReadFile("missing.txt")
			if err == nil {
				t.Fatal("read_file missing file: expected error")
			}
			assertContractOutput(t, normalizeGoldenText(t, err.Error()), golden.MissingFileError)
		})
	})
}

func TestListFilesContract(t *testing.T) {
	t.Parallel()

	var golden listFilesGolden
	readGoldenFile(t, "testdata/list_files.golden", &golden)

	t.Run("success", func(t *testing.T) {
		withFixtureRepo(t, func() {
			result, err := legacyListFiles(".")
			if err != nil {
				t.Fatalf("list_files success: %v", err)
			}
			assertContractOutput(t, normalizeGoldenText(t, result), golden.Success)
		})
	})

	t.Run("missing_path", func(t *testing.T) {
		withFixtureRepo(t, func() {
			_, err := legacyListFiles("missing-dir")
			if err == nil {
				t.Fatal("list_files missing path: expected error")
			}
			assertContractOutput(t, normalizeGoldenText(t, err.Error()), golden.MissingPathError)
		})
	})
}

func TestBashContract(t *testing.T) {
	t.Parallel()

	var golden bashGolden
	readGoldenFile(t, "testdata/bash.golden", &golden)

	t.Run("success", func(t *testing.T) {
		withFixtureRepo(t, func() {
			result, err := legacyBash(`printf 'alpha-123-beta'`)
			if err != nil {
				t.Fatalf("bash success: %v", err)
			}
			assertContractOutput(t, normalizeGoldenText(t, result), golden.Success)
		})
	})

	t.Run("non_zero_exit", func(t *testing.T) {
		withFixtureRepo(t, func() {
			result, err := legacyBash(`printf 'fail-line\n'; exit 7`)
			if err != nil {
				t.Fatalf("bash failure path should return formatted output, got error: %v", err)
			}
			assertContractOutput(t, normalizeGoldenText(t, result), golden.Failure)
		})
	})
}

func TestCodeSearchContract(t *testing.T) {
	t.Parallel()

	var golden codeSearchGolden
	readGoldenFile(t, "testdata/code_search.golden", &golden)

	t.Run("success", func(t *testing.T) {
		withFixtureRepo(t, func() {
			result, err := legacyCodeSearch(codeSearchInput{
				Pattern: `alpha-[0-9]+-beta`,
				Path:    ".",
			})
			if err != nil {
				t.Fatalf("code_search success: %v", err)
			}
			assertContractOutput(t, normalizeGoldenText(t, result), golden.Success)
		})
	})

	t.Run("no_matches", func(t *testing.T) {
		withFixtureRepo(t, func() {
			result, err := legacyCodeSearch(codeSearchInput{
				Pattern: `does-not-exist`,
				Path:    ".",
			})
			if err != nil {
				t.Fatalf("code_search no matches: %v", err)
			}
			assertContractOutput(t, normalizeGoldenText(t, result), golden.NoMatches)
		})
	})

	t.Run("missing_pattern", func(t *testing.T) {
		withFixtureRepo(t, func() {
			_, err := legacyCodeSearch(codeSearchInput{})
			if err == nil {
				t.Fatal("code_search missing pattern: expected error")
			}
			assertContractOutput(t, normalizeGoldenText(t, err.Error()), golden.MissingPatternError)
		})
	})
}

func readGoldenFile(t *testing.T, path string, target any) {
	t.Helper()

	fullPath := filepath.Join(contractsRootDir(t), path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", fullPath, err)
	}

	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("unmarshal golden %s: %v", fullPath, err)
	}
}

func assertContractOutput(t *testing.T, actual, expected string) {
	t.Helper()

	if actual != expected {
		t.Fatalf("contract mismatch\nexpected:\n%s\nactual:\n%s", expected, actual)
	}
}
