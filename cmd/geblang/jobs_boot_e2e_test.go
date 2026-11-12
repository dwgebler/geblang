package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJobsSchemaInitializesUnderConcurrentFirstBoot(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	stdlib, err := filepath.Abs("../../stdlib")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "boot.gb")
	source := `import io;
import db;
import jobs;
import sys;
let conn = db.Connection("sqlite", sys.args()[0]);
let queue = jobs.Queue(conn, {"namespace": "boot"});
io.println(queue.enqueue("work", {}).length());
`
	if err := os.WriteFile(script, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GEBLANG_STDLIB="+stdlib)
	for _, runtime := range []string{"--vm-strict", "--disable-vm"} {
		for round := 0; round < 4; round++ {
			database := filepath.Join(t.TempDir(), "jobs.db")
			outputs := make([]string, 6)
			failures := make([]error, len(outputs))
			var group sync.WaitGroup
			for i := range outputs {
				group.Add(1)
				go func(i int) {
					defer group.Done()
					ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, bin, runtime, script, database)
					cmd.Dir, cmd.Env = dir, env
					out, err := cmd.CombinedOutput()
					outputs[i], failures[i] = string(out), err
				}(i)
			}
			group.Wait()
			for i, out := range outputs {
				if failures[i] != nil || strings.TrimSpace(out) != "32" {
					t.Fatalf("%s round %d process %d: %v\n%s", runtime, round, i, failures[i], out)
				}
			}
		}
	}
}
