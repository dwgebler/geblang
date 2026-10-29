package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type testFileResult struct {
	Total   int64
	Failed  int64
	Skipped int64
	Error   string
	stdout  []byte
	stderr  []byte
}

func writeTestFileResult(path string, result testFileResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func runParallelTestFiles(files []string, jobs int, args []string) ([]testFileResult, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "geblang-test-results-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if jobs > len(files) {
		jobs = len(files)
	}
	results := make([]testFileResult, len(files))
	work := make(chan int, len(files))
	for index := range files {
		work <- index
	}
	close(work)
	var workers sync.WaitGroup
	for range jobs {
		workers.Go(func() {
			for index := range work {
				if ctx.Err() != nil {
					return
				}
				resultPath := filepath.Join(dir, fmt.Sprintf("%d.json", index))
				childArgs := append(append([]string(nil), args...), "--internal-test-result", resultPath, files[index])
				cmd := exec.CommandContext(ctx, executable, childArgs...)
				cmd.WaitDelay = 2 * time.Second
				// Each child gets its own result protocol; user output cannot forge totals.
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				cmd.Env = []string{}
				for _, value := range os.Environ() {
					if !isWorkerProfileSetting(value) {
						cmd.Env = append(cmd.Env, value)
					}
				}
				runErr := cmd.Run()
				var result testFileResult
				data, readErr := os.ReadFile(resultPath)
				if readErr == nil {
					readErr = json.Unmarshal(data, &result)
				}
				if readErr != nil {
					result.Error = fmt.Sprintf("test worker produced no valid result: %v", readErr)
					if runErr != nil {
						result.Error += ": " + runErr.Error()
					}
				} else if runErr != nil && result.Error == "" && result.Failed == 0 {
					result.Error = "test worker failed: " + runErr.Error()
				}
				result.stdout, result.stderr = stdout.Bytes(), stderr.Bytes()
				results[index] = result
			}
		})
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func isWorkerProfileSetting(value string) bool {
	return strings.HasPrefix(value, "GEBLANG_PROFILE=") || strings.HasPrefix(value, "GEBLANG_PPROF=")
}
