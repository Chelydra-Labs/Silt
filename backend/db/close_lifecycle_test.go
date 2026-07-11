package db

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"silt/backend/parser"
)

func TestClose_Idempotent(t *testing.T) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dm.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := dm.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if dm.SQLDB() != nil {
		t.Fatal("SQLDB should be nil after Close")
	}
}

func TestClose_PostCloseReturnsErrDBClosed(t *testing.T) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dm.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := dm.IsFileUnchanged("/x.md", 1, 1); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("IsFileUnchanged: want ErrDBClosed, got %v", err)
	}
	if err := dm.Checkpoint(); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("Checkpoint: want ErrDBClosed, got %v", err)
	}
	if err := dm.ForgetFile("/x.md"); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("ForgetFile: want ErrDBClosed, got %v", err)
	}
	if _, err := dm.KnownFiles(); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("KnownFiles: want ErrDBClosed, got %v", err)
	}
	if _, err := dm.handle(); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("handle: want ErrDBClosed, got %v", err)
	}
}

func TestClose_UnderConcurrentOpsNoPanic(t *testing.T) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	if err := dm.IndexFileBlocks("vault", "NB", "S", "P", []parser.ParsedBlock{sampleTaskBlock(id, 1)}, nil); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = dm.IsFileUnchanged("/nope.md", 1, 1)
					_, _ = dm.GetBlockLocation(id)
					_ = dm.IndexFileBlocks("vault", "NB", "S", "P", []parser.ParsedBlock{sampleTaskBlock(id, 1)}, nil)
				}
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	if err := dm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(stop)
	wg.Wait()

	if _, err := dm.GetBlockLocation(id); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("post-close GetBlockLocation: %v", err)
	}
}

func TestClose_WithDBWaitsForInFlight(t *testing.T) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		t.Fatal(err)
	}

	var holdWG sync.WaitGroup
	holdWG.Add(1)
	entered := make(chan struct{})
	go func() {
		_ = dm.withDB(func(db *sql.DB) error {
			close(entered)
			holdWG.Wait()
			return nil
		})
	}()
	<-entered

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- dm.Close()
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before withDB released: %v", err)
	case <-time.After(50 * time.Millisecond):
		// expected: Close blocked on write lock
	}

	holdWG.Done()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after withDB released")
	}
}
