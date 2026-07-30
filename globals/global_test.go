package globals

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

var testLinkCounter uint64

func TestReserveLinkIsAtomic(t *testing.T) {
	link := "https://example.com/concurrent-reservation"
	ReleaseLink(link)

	var successes int32
	var waitGroup sync.WaitGroup
	for i := 0; i < 100; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if ReserveLink(link) {
				atomic.AddInt32(&successes, 1)
			}
		}()
	}
	waitGroup.Wait()
	ReleaseLink(link)

	if successes != 1 {
		t.Fatalf("successful reservations = %d, want 1", successes)
	}
}

func TestCommitLinkPersistsDeliveredItem(t *testing.T) {
	tempDir := t.TempDir()
	run := atomic.AddUint64(&testLinkCounter, 1)
	link := fmt.Sprintf("https://example.com/persisted-item?run=%d", run)
	archive := filepath.Join(tempDir, "archives.txt")
	ReleaseLink(link)
	if !ReserveLink(link) {
		t.Fatal("ReserveLink() = false, want true")
	}
	if err := CommitLink(archive, link); err != nil {
		t.Fatalf("CommitLink() error = %v", err)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != link+"\n" {
		t.Fatalf("archive contents = %q, want link", data)
	}
	if ReserveLink(link) {
		t.Fatal("ReserveLink() after commit = true, want false")
	}
}
