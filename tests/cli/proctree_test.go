package cli_test

import (
	"context"
	"ghi/internal/proctree"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessTreeHelper(t *testing.T) {
	mode := os.Getenv("GHI_PROCESS_TREE_HELPER")
	if mode == "" {
		return
	}
	path := os.Getenv("GHI_PROCESS_TREE_HEARTBEAT")
	if mode == "parent" {
		child := exec.Command(os.Args[0], "-test.run=^TestProcessTreeHelper$")
		child.Env = append(os.Environ(), "GHI_PROCESS_TREE_HELPER=child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	for {
		if err := os.WriteFile(path, []byte(time.Now().String()), 0600); err != nil {
			os.Exit(3)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestProcessTreeCancellation(t *testing.T) {
	heartbeat := filepath.Join(t.TempDir(), "heartbeat")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.Command(os.Args[0], "-test.run=^TestProcessTreeHelper$")
	command.Env = append(os.Environ(), "GHI_PROCESS_TREE_HELPER=parent", "GHI_PROCESS_TREE_HEARTBEAT="+heartbeat)
	done := make(chan error, 1)
	go func() { _, err := proctree.CombinedOutput(ctx, command); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(heartbeat); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation kept inherited pipes open")
	}
	before, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	after, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("descendant survived cancellation")
	}
}
