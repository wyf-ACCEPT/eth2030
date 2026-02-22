package eftest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// TestResult holds the outcome of running a single test vector.
type TestResult struct {
	File   string
	Name   string
	Fork   string
	Index  int
	Passed bool
	Error  error
}

// BatchResult holds aggregate results for a batch of tests.
type BatchResult struct {
	Total   int
	Passed  int
	Failed  int
	Skipped int
	Errors  []*TestResult
}

// DiscoverFixtures walks a directory tree and returns paths to all .json files.
func DiscoverFixtures(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("stat directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}

	var files []string
	err = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() && strings.HasSuffix(fi.Name(), ".json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	sort.Strings(files)
	return files, nil
}

// RunSingleFixture runs all tests in a single JSON file, optionally filtered by fork.
func RunSingleFixture(path string, forkFilter string) ([]*TestResult, error) {
	tests, err := LoadStateTests(path)
	if err != nil {
		return nil, fmt.Errorf("load fixture %s: %w", path, err)
	}

	var results []*TestResult
	for name, test := range tests {
		subs := test.Subtests()
		for _, sub := range subs {
			if forkFilter != "" && sub.Fork != forkFilter {
				results = append(results, &TestResult{
					File:   path,
					Name:   name,
					Fork:   sub.Fork,
					Index:  sub.Index,
					Passed: false,
					Error:  fmt.Errorf("skipped: fork filter %q", forkFilter),
				})
				continue
			}

			if !ForkSupported(sub.Fork) {
				results = append(results, &TestResult{
					File:   path,
					Name:   name,
					Fork:   sub.Fork,
					Index:  sub.Index,
					Passed: false,
					Error:  fmt.Errorf("skipped: unsupported fork %q", sub.Fork),
				})
				continue
			}

			runResult := test.Run(sub)
			results = append(results, &TestResult{
				File:   path,
				Name:   name,
				Fork:   sub.Fork,
				Index:  sub.Index,
				Passed: runResult.Passed,
				Error:  runResult.Error,
			})
		}
	}
	return results, nil
}

// RunFixtureDir runs all fixture files in a directory, optionally filtered by fork.
func RunFixtureDir(dir string, forkFilter string) (*BatchResult, error) {
	files, err := DiscoverFixtures(dir)
	if err != nil {
		return nil, err
	}

	batch := &BatchResult{}

	for _, file := range files {
		results, err := RunSingleFixture(file, forkFilter)
		if err != nil {
			batch.Total++
			batch.Failed++
			batch.Errors = append(batch.Errors, &TestResult{
				File:  file,
				Error: err,
			})
			continue
		}

		for _, r := range results {
			batch.Total++
			if r.Error != nil && strings.HasPrefix(r.Error.Error(), "skipped:") {
				batch.Skipped++
			} else if r.Passed {
				batch.Passed++
			} else {
				batch.Failed++
				batch.Errors = append(batch.Errors, r)
			}
		}
	}

	return batch, nil
}

// RunFixtureDirConcurrent runs fixtures concurrently with the given parallelism.
func RunFixtureDirConcurrent(dir string, forkFilter string, workers int) (*BatchResult, error) {
	files, err := DiscoverFixtures(dir)
	if err != nil {
		return nil, err
	}

	if workers <= 0 {
		workers = 4
	}

	type fileResult struct {
		results []*TestResult
		err     error
		file    string
	}

	ch := make(chan string, len(files))
	for _, f := range files {
		ch <- f
	}
	close(ch)

	resultsCh := make(chan fileResult, len(files))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range ch {
				results, err := RunSingleFixture(file, forkFilter)
				resultsCh <- fileResult{results: results, err: err, file: file}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	batch := &BatchResult{}
	for fr := range resultsCh {
		if fr.err != nil {
			batch.Total++
			batch.Failed++
			batch.Errors = append(batch.Errors, &TestResult{
				File:  fr.file,
				Error: fr.err,
			})
			continue
		}
		for _, r := range fr.results {
			batch.Total++
			if r.Error != nil && strings.HasPrefix(r.Error.Error(), "skipped:") {
				batch.Skipped++
			} else if r.Passed {
				batch.Passed++
			} else {
				batch.Failed++
				batch.Errors = append(batch.Errors, r)
			}
		}
	}

	return batch, nil
}

// FormatResults returns a human-readable summary of batch results.
func FormatResults(result *BatchResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("EF State Test Results: %d total, %d passed, %d failed, %d skipped\n",
		result.Total, result.Passed, result.Failed, result.Skipped))

	if len(result.Errors) > 0 {
		sb.WriteString("\nFailures:\n")
		for i, e := range result.Errors {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(result.Errors)-20))
				break
			}
			if e.Name != "" {
				sb.WriteString(fmt.Sprintf("  [%s] %s (fork=%s, idx=%d): %v\n",
					filepath.Base(e.File), e.Name, e.Fork, e.Index, e.Error))
			} else {
				sb.WriteString(fmt.Sprintf("  [%s]: %v\n", filepath.Base(e.File), e.Error))
			}
		}
	}

	return sb.String()
}
